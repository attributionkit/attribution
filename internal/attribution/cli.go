package attribution

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
)

var cloudCommands = map[string]bool{"connect": true, "ping": true, "live-check": true, "runs": true}

type cliOptions struct {
	project    string
	json       bool
	branch     bool
	branchName string
}

func RunCLI(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		printUsage(stdout)
		return 0
	}
	if args[0] == "version" || args[0] == "--version" {
		if len(args) != 1 {
			fmt.Fprintln(stderr, "version does not accept options")
			return 2
		}
		fmt.Fprintln(stdout, "attribution "+Version)
		return 0
	}
	if cloudCommands[args[0]] {
		return runCloudCLI(args, stdout, stderr)
	}
	if args[0] == "probe" {
		return runProbeCLI(args, stdout, stderr)
	}
	command := args[0]
	if command != "init" && command != "plan" && command != "apply" && command != "verify" {
		fmt.Fprintf(stderr, "unknown command %q\n", command)
		printUsage(stderr)
		return 2
	}
	options, err := parseCLIOptions(command, args[1:])
	if err != nil {
		fmt.Fprintln(stderr, err)
		printUsage(stderr)
		return 2
	}
	root, err := filepath.Abs(options.project)
	if err != nil {
		fmt.Fprintln(stderr, "resolve project:", err)
		return 2
	}

	switch command {
	case "init":
		created, err := Init(root)
		if err != nil {
			return renderCLIError(err, stderr)
		}
		fmt.Fprintf(stdout, "Created %s (mode: %s; package manager: %s).\n", created.Path, created.Mode, created.PackageManager)
		if created.ExternalManager != "" {
			fmt.Fprintf(stdout, "%s remains the external conversion authority.\n", created.ExternalManager)
		}
		fmt.Fprintln(stdout, "WARNING: client preview only; https://attribution.sh/ is not receiving postbacks. Do not ship it to production.")
		fmt.Fprintln(stdout, "Review the desired state, then install the public Expo runtime and apply:")
		fmt.Fprintln(stdout, "  "+created.InstallCommand)
		fmt.Fprintln(stdout, "  attribution apply --branch")
		fmt.Fprintln(stdout, "Use plain `attribution apply` after committing intended dependency/config changes.")
		return 0

	case "plan":
		plan, err := BuildPlan(root)
		if err != nil {
			return renderCLIError(err, stderr)
		}
		fmt.Fprintf(stdout, "Plan for %s (Expo/%s; mode: %s):\n", plan.Project.Root, plan.Project.PackageManager, plan.Config.Mode)
		for _, operation := range plan.Operations {
			status := "change"
			if contains(plan.SyncedPaths, operation.Path) {
				status = "in sync"
			}
			fmt.Fprintf(stdout, "  %-7s %s\n", status, operation.Path)
		}
		if len(plan.ChangedPaths) == 0 {
			fmt.Fprintln(stdout, "Already in desired state (no diff). No files modified.")
		} else {
			fmt.Fprintf(stdout, "%d file(s) would change. No files modified.\n", len(plan.ChangedPaths))
		}
		return 0

	case "apply":
		// Planning (including every shape/config/package check) intentionally
		// happens before the dirty-tree guard or any mutation.
		plan, err := BuildPlan(root)
		if err != nil {
			return renderCLIError(err, stderr)
		}
		allowDirty := false
		if options.branch && len(plan.ChangedPaths) > 0 {
			name := options.branchName
			if name == "" {
				name = "attribution/setup"
			}
			if err := CreateBranch(plan.Project.Root, name); err != nil {
				return renderCLIError(err, stderr)
			}
			fmt.Fprintf(stdout, "Created branch %s.\n", name)
			allowDirty = true
		}
		result, err := Apply(plan, allowDirty)
		if err != nil {
			return renderCLIError(err, stderr)
		}
		for _, path := range result.Changed {
			fmt.Fprintf(stdout, "  wrote    %s\n", path)
		}
		for _, path := range result.Skipped {
			fmt.Fprintf(stdout, "  in sync  %s\n", path)
		}
		if len(result.Changed) == 0 {
			fmt.Fprintln(stdout, "Already in desired state (no diff).")
		} else {
			fmt.Fprintf(stdout, "Applied %d change(s).\n", len(result.Changed))
		}
		return 0

	case "verify":
		var emitter EmitFunc
		if options.json {
			writer := bufio.NewWriter(stdout)
			emitter = func(event RunEvent) error { return EncodeEvent(writer, event) }
		}
		verified, err := RunVerify(root, emitter)
		if err != nil {
			return renderCLIError(err, stderr)
		}
		if !options.json {
			renderHumanVerification(stdout, verified.Manifest)
			if verified.PersistedPath == "" {
				fmt.Fprintln(stdout, "\nRun manifest: not written (project has not been initialized).")
			} else {
				fmt.Fprintln(stdout, "\nRun manifest: "+verified.PersistedPath)
			}
		}
		if HasVerificationFailures(verified.Manifest) {
			return 1
		}
		return 0
	}
	return 70
}

func parseCLIOptions(command string, args []string) (cliOptions, error) {
	options := cliOptions{project: "."}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--project":
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "--") {
				return cliOptions{}, errors.New("--project requires a directory")
			}
			i++
			options.project = args[i]
		case "--json":
			if command != "verify" {
				return cliOptions{}, errors.New("--json is supported only by verify")
			}
			options.json = true
		case "--branch":
			if command != "apply" {
				return cliOptions{}, errors.New("--branch is supported only by apply")
			}
			options.branch = true
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
				i++
				options.branchName = args[i]
			}
		default:
			return cliOptions{}, fmt.Errorf("unknown option %q", args[i])
		}
	}
	return options, nil
}

func renderCLIError(err error, stderr io.Writer) int {
	var unsupported *UnsupportedProjectError
	var missingConfig *MissingConfigError
	var configExists *ConfigExistsError
	var missingPackage *MissingExpoPackageError
	var missingBundle *MissingBundleIdentifierError
	var invalidConfig *ConfigValidationError
	var invalidProbe *ProbeValidationError
	var dirty *DirtyWorkingTreeError
	if errors.As(err, &dirty) {
		fmt.Fprintln(stderr, dirty.Error())
		return 3
	}
	if errors.As(err, &unsupported) || errors.As(err, &missingConfig) || errors.As(err, &configExists) || errors.As(err, &missingPackage) || errors.As(err, &missingBundle) || errors.As(err, &invalidConfig) || errors.As(err, &invalidProbe) {
		fmt.Fprintln(stderr, err.Error())
		return 2
	}
	fmt.Fprintln(stderr, "internal error:", err)
	return 70
}

func renderHumanVerification(writer io.Writer, manifest RunManifest) {
	titles := map[string]string{
		"config": "Config", "build": "Build", "your-logic": "Your Logic",
		"device": "Device", "production": "Production",
	}
	order := []string{"config", "build", "your-logic", "device", "production"}
	seen := make(map[string]bool)
	for _, section := range order {
		var results []CheckResult
		for _, result := range manifest.Results {
			if result.Section == section {
				results = append(results, result)
			}
		}
		if len(results) == 0 {
			continue
		}
		seen[section] = true
		fmt.Fprintln(writer, "\n"+titles[section])
		for _, result := range results {
			renderHumanResult(writer, result)
		}
	}
	var unknownSections []string
	for _, result := range manifest.Results {
		if !seen[result.Section] && !contains(unknownSections, result.Section) {
			unknownSections = append(unknownSections, result.Section)
		}
	}
	sort.Strings(unknownSections)
	for _, section := range unknownSections {
		fmt.Fprintln(writer, "\n"+section)
		for _, result := range manifest.Results {
			if result.Section == section {
				renderHumanResult(writer, result)
			}
		}
	}
}

func renderHumanResult(writer io.Writer, result CheckResult) {
	mark := "·"
	if result.Execution != "succeeded" {
		mark = "!"
	} else if result.Verdict == "pass" {
		mark = "✓"
	} else if result.Verdict == "fail" {
		mark = "✗"
	}
	fmt.Fprintf(writer, "  %s %s  [execution: %s · verdict: %s · evidence: %s · basis: %s · integrity: %s · comparability: %s · collection: %s · finality: %s]\n      %s\n", mark, result.CheckID, result.Execution, result.Verdict, result.Evidence, result.Basis, result.Integrity, result.Comparability, result.CollectionHealth, result.Finality, result.Reason)
	if result.Remediation != "" {
		fmt.Fprintln(writer, "      → "+result.Remediation)
	}
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func printUsage(writer io.Writer) {
	fmt.Fprintln(writer, "usage: attribution <init|plan|apply|probe import|verify|connect|runs upload|ping|live-check|version> [options]")
}

package attribution

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	RuntimeProbeSchemaVersion = "1.0.0"
	probeReportMaxBytes       = 16 * 1024
	probeArtifactMaxBytes     = 64 * 1024
)

var (
	probeFreshness  = 15 * time.Minute
	probeFutureSkew = time.Minute
	sha256Pattern   = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

type ProbeImportOptions struct {
	Framework string
	Target    string
	Report    string
}

type ProbeBackendObservation struct {
	Status string `json:"status"`
}

type ProbeReportObservation struct {
	Event               string                  `json:"event"`
	FineConversionValue int                     `json:"fineConversionValue"`
	SchemaHash          string                  `json:"schemaHash"`
	AdAttributionKit    ProbeBackendObservation `json:"adAttributionKit"`
	SKAdNetwork         ProbeBackendObservation `json:"skAdNetwork"`
}

type ProbeSource struct {
	SHA256     string `json:"sha256"`
	ByteLength int    `json:"byteLength"`
	ModifiedAt string `json:"modifiedAt"`
}

type ProbeProjectBinding struct {
	BundleID   string  `json:"bundleId"`
	ConfigHash string  `json:"configHash"`
	SchemaHash string  `json:"schemaHash"`
	PlanHash   string  `json:"planHash"`
	Revision   *string `json:"revision"`
}

type RuntimeProbeArtifact struct {
	SchemaVersion string                 `json:"schemaVersion"`
	Kind          string                 `json:"kind"`
	Framework     string                 `json:"framework"`
	Target        string                 `json:"target"`
	ImportedAt    string                 `json:"importedAt"`
	ExpiresAt     string                 `json:"expiresAt"`
	Source        ProbeSource            `json:"source"`
	Project       ProbeProjectBinding    `json:"project"`
	Report        ProbeReportObservation `json:"report"`
}

type runtimeBackendWire struct {
	Status *string         `json:"status"`
	Error  json.RawMessage `json:"error,omitempty"`
}

type runtimeReportWire struct {
	Event               *string             `json:"event"`
	FineConversionValue *int                `json:"fineConversionValue"`
	SchemaHash          *string             `json:"schemaHash"`
	AdAttributionKit    *runtimeBackendWire `json:"adAttributionKit"`
	SKAdNetwork         *runtimeBackendWire `json:"skAdNetwork"`
}

type staleProbeError struct {
	reason string
}

func (e *staleProbeError) Error() string { return e.reason }

func runProbeCLI(args []string, stdout, stderr io.Writer) int {
	if len(args) < 2 || args[0] != "probe" || args[1] != "import" {
		fmt.Fprintln(stderr, "probe requires the import subcommand")
		printProbeUsage(stderr)
		return 2
	}
	options, project, err := parseProbeImportOptions(args[2:])
	if err != nil {
		fmt.Fprintln(stderr, err)
		printProbeUsage(stderr)
		return 2
	}
	root, err := filepath.Abs(project)
	if err != nil {
		fmt.Fprintln(stderr, "resolve project:", err)
		return 2
	}
	artifact, err := ImportRuntimeProbe(root, options)
	if err != nil {
		return renderCLIError(err, stderr)
	}
	fmt.Fprintf(stdout, "Imported fresh %s %s runtime report for %s (conversion value %d).\n", artifact.Framework, artifact.Target, artifact.Report.Event, artifact.Report.FineConversionValue)
	fmt.Fprintf(stdout, "Stored non-secret %s with exact-source SHA-256 %s.\n", ProbePath, artifact.Source.SHA256)
	fmt.Fprintln(stdout, "Run `attribution verify --json` next. This probe can affect only Your Logic; Device and Production remain unknown.")
	return 0
}

func printProbeUsage(writer io.Writer) {
	fmt.Fprintln(writer, "usage: attribution probe import --framework <expo|swiftui> --target simulator --report <path> [--project <directory>]")
}

func parseProbeImportOptions(args []string) (ProbeImportOptions, string, error) {
	options := ProbeImportOptions{}
	project := "."
	seen := map[string]bool{}
	for i := 0; i < len(args); i++ {
		name := args[i]
		if name != "--framework" && name != "--target" && name != "--report" && name != "--project" {
			return ProbeImportOptions{}, "", fmt.Errorf("unknown probe option %q", name)
		}
		if seen[name] {
			return ProbeImportOptions{}, "", fmt.Errorf("probe option %s may be provided only once", name)
		}
		seen[name] = true
		if i+1 >= len(args) || strings.HasPrefix(args[i+1], "--") {
			return ProbeImportOptions{}, "", fmt.Errorf("%s requires a value", name)
		}
		i++
		switch name {
		case "--framework":
			options.Framework = args[i]
		case "--target":
			options.Target = args[i]
		case "--report":
			options.Report = args[i]
		case "--project":
			project = args[i]
		}
	}
	if options.Framework == "" || options.Target == "" || options.Report == "" {
		return ProbeImportOptions{}, "", errors.New("--framework, --target, and --report are required")
	}
	return options, project, nil
}

func ImportRuntimeProbe(root string, options ProbeImportOptions) (RuntimeProbeArtifact, error) {
	return importRuntimeProbeAt(root, options, time.Now().UTC())
}

func importRuntimeProbeAt(root string, options ProbeImportOptions, now time.Time) (RuntimeProbeArtifact, error) {
	if options.Framework != "expo" && options.Framework != "swiftui" {
		return RuntimeProbeArtifact{}, rejectProbe(`--framework must be "expo" or "swiftui"`)
	}
	if options.Target != "simulator" {
		return RuntimeProbeArtifact{}, rejectProbe(`--target must be "simulator"; device and production evidence cannot be imported by this command`)
	}
	if strings.TrimSpace(options.Report) == "" {
		return RuntimeProbeArtifact{}, rejectProbe("--report must name a fresh runtime JSON file")
	}
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return RuntimeProbeArtifact{}, fmt.Errorf("resolve project: %w", err)
	}
	rootInfo, err := os.Stat(absoluteRoot)
	if err != nil || !rootInfo.IsDir() {
		return RuntimeProbeArtifact{}, rejectProbe("project path is not a directory")
	}

	config, configRaw, err := ReadConfig(absoluteRoot)
	if err != nil {
		return RuntimeProbeArtifact{}, err
	}
	manifest, manifestRaw, err := readProbeManifest(absoluteRoot)
	if err != nil {
		return RuntimeProbeArtifact{}, rejectProbe(err.Error())
	}
	expectedFramework := manifest.Host
	if expectedFramework != "expo" && expectedFramework != "swiftui" {
		return RuntimeProbeArtifact{}, rejectProbe("generated plan host is unsupported")
	}
	if options.Framework != expectedFramework {
		return RuntimeProbeArtifact{}, rejectProbe(fmt.Sprintf("--framework %s does not match the generated %s host plan", options.Framework, expectedFramework))
	}
	configHash := sha256Hex(configRaw)
	schemaDigest := schemaHash(config)
	if err := validateProbePlan(absoluteRoot, config, configHash, schemaDigest, manifest); err != nil {
		return RuntimeProbeArtifact{}, rejectProbe(err.Error())
	}

	raw, modifiedAt, err := readFreshProbeReport(options.Report, now)
	if err != nil {
		return RuntimeProbeArtifact{}, rejectProbe(err.Error())
	}
	report, err := decodeRuntimeReport(raw)
	if err != nil {
		return RuntimeProbeArtifact{}, rejectProbe(err.Error())
	}
	if err := validateRuntimeReport(report, config, schemaDigest, options.Target); err != nil {
		return RuntimeProbeArtifact{}, rejectProbe(err.Error())
	}

	artifact := RuntimeProbeArtifact{
		SchemaVersion: RuntimeProbeSchemaVersion,
		Kind:          "attribution-update-report",
		Framework:     options.Framework,
		Target:        options.Target,
		ImportedAt:    now.Format(time.RFC3339Nano),
		ExpiresAt:     now.Add(probeFreshness).Format(time.RFC3339Nano),
		Source: ProbeSource{
			SHA256:     sha256Hex(raw),
			ByteLength: len(raw),
			ModifiedAt: modifiedAt.UTC().Format(time.RFC3339Nano),
		},
		Project: ProbeProjectBinding{
			BundleID:   config.App.BundleID,
			ConfigHash: configHash,
			SchemaHash: schemaDigest,
			PlanHash:   sha256Hex(manifestRaw),
			Revision:   gitRevision(absoluteRoot),
		},
		Report: report,
	}
	encoded, err := json.MarshalIndent(artifact, "", "  ")
	if err != nil {
		return RuntimeProbeArtifact{}, fmt.Errorf("encode runtime probe artifact: %w", err)
	}
	if err := safeAtomicWrite(absoluteRoot, ProbePath, append(encoded, '\n')); err != nil {
		return RuntimeProbeArtifact{}, fmt.Errorf("persist runtime probe: %w", err)
	}
	return artifact, nil
}

func rejectProbe(problem string) error {
	return &ProbeValidationError{Problem: problem}
}

func readProbeManifest(root string) (GeneratedManifest, []byte, error) {
	if err := validateSafeTarget(root, ManifestPath); err != nil {
		return GeneratedManifest{}, nil, err
	}
	path := filepath.Join(root, filepath.FromSlash(ManifestPath))
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return GeneratedManifest{}, nil, fmt.Errorf("%s not found; run `attribution plan` and `attribution apply` first", ManifestPath)
	}
	if err != nil {
		return GeneratedManifest{}, nil, fmt.Errorf("inspect %s: %w", ManifestPath, err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() <= 0 || info.Size() > probeArtifactMaxBytes {
		return GeneratedManifest{}, nil, fmt.Errorf("%s must be a bounded regular non-symlink file", ManifestPath)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return GeneratedManifest{}, nil, fmt.Errorf("read %s: %w", ManifestPath, err)
	}
	var manifest GeneratedManifest
	if err := decodeStrictJSON(raw, &manifest); err != nil {
		return GeneratedManifest{}, nil, fmt.Errorf("invalid %s: %w", ManifestPath, err)
	}
	return manifest, raw, nil
}

func validateProbePlan(root string, config Config, configHash, schemaDigest string, manifest GeneratedManifest) error {
	project, err := DiscoverProject(root)
	if err != nil {
		return err
	}
	if project.Host == "swiftui" {
		return validateSwiftProbePlan(project, config, configHash, schemaDigest, manifest)
	}
	if manifest.Version != 1 || manifest.GeneratedBy != "attribution "+Version || manifest.Host != "expo" || manifest.Mode != config.Mode || manifest.PackageManager != project.PackageManager || manifest.ConfigHash != configHash || manifest.SchemaHash != schemaDigest {
		return errors.New("generated plan metadata or config/schema hash is stale; run `attribution plan` and `attribution apply`")
	}
	if project.BundleID != config.App.BundleID || manifest.AppConfig != (GeneratedAppConfig{Path: "app.json", Plugin: "./" + PluginPath}) {
		return errors.New("generated plan does not match the current app identity or plugin registration; run `attribution apply`")
	}
	if err := validateSafeTarget(root, ".attribution/.gitignore"); err != nil {
		return err
	}
	ignoreRaw, err := os.ReadFile(filepath.Join(root, ".attribution", ".gitignore"))
	if err != nil || !bytes.Equal(ignoreRaw, []byte("last-run.json\nprobe.json\n")) {
		return errors.New("local runtime artifacts are not in the generated ignore file; run `attribution apply`")
	}
	expectedWrapper, err := renderWrapper(config, schemaDigest)
	if err != nil {
		return fmt.Errorf("compile expected runtime plan: %w", err)
	}
	if len(manifest.GeneratedFiles) != 1 || manifest.GeneratedFiles[0].Path != PluginPath || manifest.GeneratedFiles[0].SHA256 != sha256Hex(expectedWrapper) {
		return errors.New("generated plan does not bind the expected runtime wrapper; run `attribution apply`")
	}
	if err := validateSafeTarget(root, PluginPath); err != nil {
		return err
	}
	wrapper, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(PluginPath)))
	if err != nil {
		return fmt.Errorf("read generated runtime wrapper: %w", err)
	}
	if !bytes.Equal(wrapper, expectedWrapper) {
		return errors.New("generated runtime wrapper drifted from the current config; run `attribution apply`")
	}
	return nil
}

func validateSwiftProbePlan(project Project, config Config, configHash, schemaDigest string, manifest GeneratedManifest) error {
	if manifest.Version != 1 || manifest.GeneratedBy != "attribution "+Version || manifest.Host != "swiftui" || manifest.Mode != config.Mode || manifest.PackageManager != "swiftpm" || manifest.ConfigHash != configHash || manifest.SchemaHash != schemaDigest {
		return errors.New("generated SwiftUI plan metadata or config/schema hash is stale; run `attribution plan` and `attribution apply`")
	}
	expectedApp := GeneratedAppConfig{Path: project.SwiftUI.ProjectFile, Target: project.SwiftUI.TargetName, InfoPlist: project.SwiftUI.InfoPlistPath, GeneratedSwift: SwiftSourcePath, PackageProduct: "AttributionCore"}
	if project.BundleID != config.App.BundleID || manifest.AppConfig != expectedApp {
		return errors.New("generated SwiftUI plan does not match the current app identity or Xcode target binding; run `attribution apply`")
	}
	if err := validateSafeTarget(project.Root, ".attribution/.gitignore"); err != nil {
		return err
	}
	ignoreRaw, err := os.ReadFile(filepath.Join(project.Root, ".attribution", ".gitignore"))
	if err != nil || !bytes.Equal(ignoreRaw, []byte("last-run.json\nprobe.json\n")) {
		return errors.New("local runtime artifacts are not in the generated ignore file; run `attribution apply`")
	}
	plist, err := renderSwiftPlist(config, schemaDigest)
	if err != nil {
		return fmt.Errorf("compile expected SwiftUI plist plan: %w", err)
	}
	expected := []struct {
		path string
		raw  []byte
	}{
		{SwiftSourcePath, renderSwiftSource(config, schemaDigest)},
		{SwiftPlistPath, plist},
		{SwiftGuidePath, renderSwiftGuide(project)},
	}
	if len(manifest.GeneratedFiles) != len(expected) {
		return errors.New("generated SwiftUI plan does not bind the expected owned artifacts; run `attribution apply`")
	}
	for index, item := range expected {
		if err := validateSafeTarget(project.Root, item.path); err != nil {
			return err
		}
		observed, readErr := os.ReadFile(filepath.Join(project.Root, filepath.FromSlash(item.path)))
		if readErr != nil || !bytes.Equal(observed, item.raw) || manifest.GeneratedFiles[index].Path != item.path || manifest.GeneratedFiles[index].SHA256 != sha256Hex(observed) {
			return fmt.Errorf("generated SwiftUI artifact drifted at %s; run `attribution apply`", item.path)
		}
	}
	integration := inspectSwiftUIIntegration(project)
	if !integration.PackageLinked {
		return errors.New(integration.PackageProblem)
	}
	if !integration.SourceTargeted {
		return errors.New(integration.SourceProblem)
	}
	info, err := loadSwiftInfoPlist(project)
	if err != nil {
		return err
	}
	if observed, ok := info["AttributionKitSchemaHash"].(string); !ok || observed != schemaDigest {
		return errors.New("target Info.plist schema hash does not match the generated SwiftUI plan")
	}
	events, ok := info["AttributionKitEventValues"].(map[string]any)
	if !ok || len(events) != len(config.Schema.Events) {
		return errors.New("target Info.plist event values do not match the generated SwiftUI plan")
	}
	for index, event := range config.Schema.Events {
		if value, found := events[event].(int); !found || value != index {
			return errors.New("target Info.plist event values do not match the generated SwiftUI plan")
		}
	}
	expectedEndpoint, _ := normalizedEndpoint(config.Providers.Apple.Endpoint)
	if observed, ok := info["NSAdvertisingAttributionReportEndpoint"].(string); !ok || observed != expectedEndpoint {
		return errors.New("target Info.plist endpoint does not match the generated SwiftUI plan")
	}
	if len(config.Providers.Apple.SKAdNetworkIDs) > 0 {
		items, ok := info["SKAdNetworkItems"].([]any)
		if !ok {
			return errors.New("target Info.plist SKAdNetworkItems do not match the generated SwiftUI plan")
		}
		observedIDs := map[string]bool{}
		for _, item := range items {
			if entry, ok := item.(map[string]any); ok {
				if id, ok := entry["SKAdNetworkIdentifier"].(string); ok {
					observedIDs[id] = true
				}
			}
		}
		for _, id := range config.Providers.Apple.SKAdNetworkIDs {
			if !observedIDs[id] {
				return errors.New("target Info.plist SKAdNetworkItems do not match the generated SwiftUI plan")
			}
		}
	}
	return nil
}

func readFreshProbeReport(path string, now time.Time) ([]byte, time.Time, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("resolve report path: %w", err)
	}
	before, err := os.Lstat(absolute)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("inspect report: %w", err)
	}
	if !before.Mode().IsRegular() || before.Mode()&os.ModeSymlink != 0 {
		return nil, time.Time{}, errors.New("report must be a regular non-symlink file")
	}
	if before.Size() <= 0 || before.Size() > probeReportMaxBytes {
		return nil, time.Time{}, fmt.Errorf("report must contain 1 to %d bytes", probeReportMaxBytes)
	}
	modifiedAt := before.ModTime().UTC()
	if modifiedAt.Before(now.Add(-probeFreshness)) {
		return nil, time.Time{}, fmt.Errorf("report is stale (older than %s)", probeFreshness)
	}
	if modifiedAt.After(now.Add(probeFutureSkew)) {
		return nil, time.Time{}, errors.New("report modification time is implausibly in the future")
	}

	file, err := os.Open(absolute)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("open report: %w", err)
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("inspect open report: %w", err)
	}
	if !os.SameFile(before, opened) {
		return nil, time.Time{}, errors.New("report changed while it was being opened")
	}
	raw, err := io.ReadAll(io.LimitReader(file, probeReportMaxBytes+1))
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("read report: %w", err)
	}
	if len(raw) == 0 || len(raw) > probeReportMaxBytes {
		return nil, time.Time{}, fmt.Errorf("report must contain 1 to %d bytes", probeReportMaxBytes)
	}
	after, err := file.Stat()
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("reinspect report: %w", err)
	}
	if opened.Size() != after.Size() || !opened.ModTime().Equal(after.ModTime()) {
		return nil, time.Time{}, errors.New("report changed while it was being read")
	}
	return raw, modifiedAt, nil
}

func decodeRuntimeReport(raw []byte) (ProbeReportObservation, error) {
	var wire runtimeReportWire
	if err := decodeStrictJSON(raw, &wire); err != nil {
		return ProbeReportObservation{}, fmt.Errorf("report is not strict AttributionUpdateReport JSON: %w", err)
	}
	if wire.Event == nil || wire.FineConversionValue == nil || wire.SchemaHash == nil || wire.AdAttributionKit == nil || wire.SKAdNetwork == nil {
		return ProbeReportObservation{}, errors.New("report is missing a required AttributionUpdateReport field")
	}
	aak, err := validateRuntimeBackend("adAttributionKit", wire.AdAttributionKit)
	if err != nil {
		return ProbeReportObservation{}, err
	}
	skan, err := validateRuntimeBackend("skAdNetwork", wire.SKAdNetwork)
	if err != nil {
		return ProbeReportObservation{}, err
	}
	return ProbeReportObservation{
		Event:               *wire.Event,
		FineConversionValue: *wire.FineConversionValue,
		SchemaHash:          *wire.SchemaHash,
		AdAttributionKit:    ProbeBackendObservation{Status: aak},
		SKAdNetwork:         ProbeBackendObservation{Status: skan},
	}, nil
}

func validateRuntimeBackend(name string, backend *runtimeBackendWire) (string, error) {
	if backend.Status == nil {
		return "", fmt.Errorf("%s.status is required", name)
	}
	status := *backend.Status
	if status != "succeeded" && status != "failed" && status != "unavailable" {
		return "", fmt.Errorf("%s.status is not one of succeeded, failed, or unavailable", name)
	}
	if status == "failed" {
		var message string
		if len(backend.Error) == 0 || json.Unmarshal(backend.Error, &message) != nil || strings.TrimSpace(message) == "" || len(message) > 4096 || strings.ContainsRune(message, '\x00') {
			return "", fmt.Errorf("%s failed status requires a non-empty bounded error string", name)
		}
	} else if len(backend.Error) != 0 {
		return "", fmt.Errorf("%s %s status must not carry an error", name, status)
	}
	return status, nil
}

func validateRuntimeReport(report ProbeReportObservation, config Config, schemaDigest, target string) error {
	for _, backend := range []ProbeBackendObservation{report.AdAttributionKit, report.SKAdNetwork} {
		if backend.Status != "failed" && backend.Status != "unavailable" {
			if target == "simulator" && backend.Status == "succeeded" {
				return errors.New("simulator report claimed an Apple backend succeeded; this command accepts only failed or unavailable simulator outcomes")
			}
			return errors.New("report contains a backend status not emitted for an accepted simulator probe")
		}
	}
	expectedValue := -1
	for index, event := range config.Schema.Events {
		if event == report.Event {
			expectedValue = index
			break
		}
	}
	if expectedValue < 0 {
		return errors.New("report event is not declared in the current project schema")
	}
	if report.FineConversionValue != expectedValue {
		return fmt.Errorf("reported conversion value %d does not match the current generated plan value %d", report.FineConversionValue, expectedValue)
	}
	if report.SchemaHash != schemaDigest {
		return errors.New("report schemaHash does not match the current config and generated plan")
	}
	return nil
}

func decodeStrictJSON(raw []byte, target any) error {
	if err := rejectDuplicateJSONKeys(raw); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func rejectDuplicateJSONKeys(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	var visit func() error
	visit = func() error {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delimiter, isDelimiter := token.(json.Delim)
		if !isDelimiter {
			return nil
		}
		switch delimiter {
		case '{':
			seen := map[string]bool{}
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return err
				}
				key, ok := keyToken.(string)
				if !ok {
					return errors.New("object key is not a string")
				}
				if seen[key] {
					return fmt.Errorf("duplicate JSON key %q", key)
				}
				seen[key] = true
				if err := visit(); err != nil {
					return err
				}
			}
			closing, err := decoder.Token()
			if err != nil || closing != json.Delim('}') {
				return errors.New("unterminated JSON object")
			}
		case '[':
			for decoder.More() {
				if err := visit(); err != nil {
					return err
				}
			}
			closing, err := decoder.Token()
			if err != nil || closing != json.Delim(']') {
				return errors.New("unterminated JSON array")
			}
		default:
			return fmt.Errorf("unexpected JSON delimiter %q", delimiter)
		}
		return nil
	}
	if err := visit(); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func loadRuntimeProbe(root string) (*RuntimeProbeArtifact, string) {
	if err := validateSafeTarget(root, ProbePath); err != nil {
		return nil, err.Error()
	}
	path := filepath.Join(root, filepath.FromSlash(ProbePath))
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ""
	}
	if err != nil {
		return nil, fmt.Sprintf("inspect %s: %v", ProbePath, err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() <= 0 || info.Size() > probeArtifactMaxBytes {
		return nil, ProbePath + " must be a bounded regular non-symlink file"
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Sprintf("read %s: %v", ProbePath, err)
	}
	var artifact RuntimeProbeArtifact
	if err := decodeStrictJSON(raw, &artifact); err != nil {
		return nil, fmt.Sprintf("invalid %s: %v", ProbePath, err)
	}
	return &artifact, ""
}

func validateStoredProbe(artifact RuntimeProbeArtifact, obs observation, now time.Time) error {
	if artifact.SchemaVersion != RuntimeProbeSchemaVersion || artifact.Kind != "attribution-update-report" {
		return errors.New("probe artifact schema or kind is unsupported")
	}
	if artifact.Framework != "expo" && artifact.Framework != "swiftui" {
		return errors.New("probe artifact framework is invalid")
	}
	if obs.project.Host != artifact.Framework {
		return errors.New("probe artifact framework does not match the current project host")
	}
	if artifact.Target != "simulator" {
		return errors.New("probe artifact target is not simulator")
	}
	importedAt, err := time.Parse(time.RFC3339Nano, artifact.ImportedAt)
	if err != nil {
		return errors.New("probe artifact importedAt is invalid")
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, artifact.ExpiresAt)
	if err != nil || !expiresAt.Equal(importedAt.Add(probeFreshness)) {
		return errors.New("probe artifact expiry is invalid")
	}
	modifiedAt, err := time.Parse(time.RFC3339Nano, artifact.Source.ModifiedAt)
	if err != nil || modifiedAt.Before(importedAt.Add(-probeFreshness)) || modifiedAt.After(importedAt.Add(probeFutureSkew)) {
		return errors.New("probe source freshness binding is invalid")
	}
	if importedAt.After(now.Add(probeFutureSkew)) {
		return errors.New("probe import time is implausibly in the future")
	}
	if now.After(expiresAt) {
		return &staleProbeError{reason: fmt.Sprintf("probe expired after %s", probeFreshness)}
	}
	if !sha256Pattern.MatchString(artifact.Source.SHA256) || artifact.Source.ByteLength <= 0 || artifact.Source.ByteLength > probeReportMaxBytes {
		return errors.New("probe exact-source binding is invalid")
	}
	if obs.config == nil || obs.manifest == nil {
		return errors.New("current config or generated plan is unavailable")
	}
	configHash := sha256Hex(obs.configRaw)
	schemaDigest := schemaHash(*obs.config)
	if err := validateProbePlan(obs.project.Root, *obs.config, configHash, schemaDigest, *obs.manifest); err != nil {
		return fmt.Errorf("current generated plan is invalid: %w", err)
	}
	if artifact.Project.BundleID != obs.config.App.BundleID || artifact.Project.ConfigHash != configHash || artifact.Project.SchemaHash != schemaDigest || artifact.Report.SchemaHash != schemaDigest {
		return errors.New("probe no longer matches the current project config or schema")
	}
	if !sha256Pattern.MatchString(artifact.Project.PlanHash) || artifact.Project.PlanHash != sha256Hex(obs.manifestRaw) {
		return errors.New("probe no longer matches the exact generated plan bytes")
	}
	if obs.manifest.ConfigHash != configHash || obs.manifest.SchemaHash != schemaDigest {
		return errors.New("probe no longer matches the current generated plan")
	}
	if !sameOptionalString(artifact.Project.Revision, gitRevision(obs.project.Root)) {
		return errors.New("probe was imported for a different project revision")
	}
	if err := validateRuntimeReport(artifact.Report, *obs.config, schemaDigest, artifact.Target); err != nil {
		return err
	}
	return nil
}

func sameOptionalString(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

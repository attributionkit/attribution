package attribution

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type Operation struct {
	Path    string
	Content []byte
}

type Plan struct {
	Project      Project
	Config       Config
	ConfigHash   string
	SchemaHash   string
	Operations   []Operation
	ChangedPaths []string
	SyncedPaths  []string
}

type GeneratedFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type GeneratedManifest struct {
	Version        int                `json:"version"`
	GeneratedBy    string             `json:"generatedBy"`
	Host           string             `json:"host"`
	PackageManager string             `json:"packageManager"`
	Mode           string             `json:"mode"`
	ConfigHash     string             `json:"configHash"`
	SchemaHash     string             `json:"schemaHash"`
	GeneratedFiles []GeneratedFile    `json:"generatedFiles"`
	AppConfig      GeneratedAppConfig `json:"appConfig"`
}

type GeneratedAppConfig struct {
	Path           string `json:"path"`
	Plugin         string `json:"plugin,omitempty"`
	Target         string `json:"target,omitempty"`
	InfoPlist      string `json:"infoPlist,omitempty"`
	GeneratedSwift string `json:"generatedSwift,omitempty"`
	PackageProduct string `json:"packageProduct,omitempty"`
}

type ApplyResult struct {
	Changed []string
	Skipped []string
}

type InitResult struct {
	Path            string
	Host            string
	Mode            string
	PackageManager  string
	InstallCommand  string
	ExternalManager string
}

func Init(root string) (InitResult, error) {
	project, err := DiscoverProject(root)
	if err != nil {
		return InitResult{}, err
	}
	target := filepath.Join(project.Root, filepath.FromSlash(ConfigPath))
	if _, err := os.Lstat(target); err == nil {
		return InitResult{}, &ConfigExistsError{Root: project.Root}
	} else if !errors.Is(err, os.ErrNotExist) {
		return InitResult{}, fmt.Errorf("inspect %s: %w", ConfigPath, err)
	}
	if project.BundleID == "" {
		return InitResult{}, &MissingBundleIdentifierError{}
	}

	mode := "managed"
	owner := "managed-runtime"
	externalName := ""
	transports := []string{}
	if project.Host == "expo" {
		for _, manager := range installedManagers(project) {
			if manager.Disableable {
				transports = append(transports, manager.Package)
				continue
			}
			if mode == "managed" {
				mode = "external"
				owner = manager.Name
				externalName = manager.Name
			}
		}
	}
	sort.Strings(transports)

	bundleID := project.BundleID

	lines := []string{
		"# attribution.sh desired state — human-owned. Edit this file; `attribution apply` compiles it.",
		"# CLIENT PREVIEW: https://attribution.sh is not receiving postbacks. Do not ship this endpoint to production.",
		"version: 1",
		"mode: " + mode,
		"app:",
		"  bundleId: " + quoteYAML(bundleID),
		"conversionAuthority:",
		"  owner: " + quoteYAML(owner),
	}
	if len(transports) == 0 {
		lines = append(lines, "eventTransports: []")
	} else {
		lines = append(lines, "eventTransports:")
		for _, transport := range transports {
			lines = append(lines, "  - "+quoteYAML(transport))
		}
	}
	lines = append(lines,
		"providers:",
		"  apple:",
		"    endpoint: https://attribution.sh",
		"    # Add only the SKAdNetwork ids for ad networks you use:",
		"    skAdNetworkIds: []",
	)
	if project.Host == "expo" {
		if _, hasMeta := project.Dependencies["react-native-fbsdk-next"]; hasMeta {
			lines = append(lines,
				"  # Meta SDK detected. Uncomment only after replacing the placeholder with the real numeric app id:",
				"  # meta:",
				"  #   appId: \"<replace-with-real-meta-app-id>\"",
			)
		} else {
			lines = append(lines,
				"  # meta:",
				"  #   appId: \"<replace-with-real-meta-app-id>\"",
			)
		}
	} else {
		lines = append(lines,
			"  # Native provider SDKs remain human-owned; this preview compiles only Apple settings.",
		)
	}
	lines = append(lines,
		"schema:",
		"  events:",
		"    - install",
		"    - trial",
		"    - purchase",
		"    - retention",
		"",
	)
	if err := safeAtomicWrite(project.Root, ConfigPath, []byte(strings.Join(lines, "\n"))); err != nil {
		return InitResult{}, err
	}
	return InitResult{
		Path:            ConfigPath,
		Host:            project.Host,
		Mode:            mode,
		PackageManager:  project.PackageManager,
		InstallCommand:  installCommandForProject(project),
		ExternalManager: externalName,
	}, nil
}

func installCommandForProject(project Project) string {
	if project.Host == "swiftui" {
		return "In Xcode, add " + AttributionRepo + " and link the AttributionCore product to target " + project.SwiftUI.TargetName + "."
	}
	return installCommand(project.PackageManager)
}

func quoteYAML(value string) string {
	raw, _ := json.Marshal(value)
	return string(raw)
}

func installCommand(manager string) string {
	packageSpec := ReleaseBaseURL + "/v" + strings.TrimPrefix(Version, "v") + "/attributionkit-expo-" + strings.TrimPrefix(Version, "v") + ".tgz"
	switch manager {
	case "yarn":
		return "yarn add " + packageSpec
	case "pnpm":
		return "pnpm add " + packageSpec
	case "bun":
		return "bun add " + packageSpec
	default:
		return "npm install " + packageSpec
	}
}

func BuildPlan(root string) (Plan, error) {
	project, err := DiscoverProject(root)
	if err != nil {
		return Plan{}, err
	}
	if project.Host == "swiftui" {
		return buildSwiftUIPlan(project)
	}
	return buildExpoPlan(project)
}

func buildExpoPlan(project Project) (Plan, error) {
	root := project.Root
	for _, path := range []string{"app.json", ConfigPath, ".attribution/.gitignore", PluginPath, ManifestPath} {
		if err := validateSafeTarget(root, path); err != nil {
			return Plan{}, err
		}
	}
	config, rawConfig, err := ReadConfig(project.Root)
	if err != nil {
		return Plan{}, err
	}
	if !attributionPackageInstalled(project) {
		return Plan{}, &MissingExpoPackageError{}
	}
	if project.BundleID == "" {
		return Plan{}, &MissingBundleIdentifierError{}
	}
	if config.App.BundleID != project.BundleID {
		return Plan{}, &ConfigValidationError{Problems: []string{fmt.Sprintf("app.bundleId %q does not match app.json expo.ios.bundleIdentifier %q", config.App.BundleID, project.BundleID)}}
	}
	if config.Mode == "external" {
		matched := false
		for _, manager := range installedManagers(project) {
			if managerMatchesOwner(manager, config.ConversionAuthority.Owner) && managerPackageInstalled(project, manager) {
				matched = true
				break
			}
		}
		if !matched {
			return Plan{}, &ConfigValidationError{Problems: []string{fmt.Sprintf("external conversionAuthority.owner %q must correspond to an installed known conversion manager", config.ConversionAuthority.Owner)}}
		}
	}

	configDigest := sha256Hex(rawConfig)
	schemaDigest := schemaHash(config)
	wrapper, err := renderWrapper(config, schemaDigest)
	if err != nil {
		return Plan{}, err
	}
	appJSON, err := appJSONWithPlugin(project.AppJSON)
	if err != nil {
		return Plan{}, fmt.Errorf("plan app.json registration: %w", err)
	}
	appRaw, err := json.MarshalIndent(appJSON, "", "  ")
	if err != nil {
		return Plan{}, fmt.Errorf("encode app.json: %w", err)
	}
	appRaw = append(appRaw, '\n')

	manifest := GeneratedManifest{
		Version:        1,
		GeneratedBy:    "attribution " + Version,
		Host:           "expo",
		PackageManager: project.PackageManager,
		Mode:           config.Mode,
		ConfigHash:     configDigest,
		SchemaHash:     schemaDigest,
		GeneratedFiles: []GeneratedFile{{Path: PluginPath, SHA256: sha256Hex(wrapper)}},
	}
	manifest.AppConfig.Path = "app.json"
	manifest.AppConfig.Plugin = "./" + PluginPath
	manifestRaw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return Plan{}, fmt.Errorf("encode manifest: %w", err)
	}
	manifestRaw = append(manifestRaw, '\n')

	operations := []Operation{
		{Path: ".attribution/.gitignore", Content: []byte("last-run.json\nprobe.json\n")},
		{Path: PluginPath, Content: wrapper},
		{Path: ManifestPath, Content: manifestRaw},
		{Path: "app.json", Content: appRaw},
	}
	plan := Plan{
		Project:    project,
		Config:     config,
		ConfigHash: configDigest,
		SchemaHash: schemaDigest,
		Operations: operations,
	}
	for _, operation := range operations {
		current, err := os.ReadFile(filepath.Join(project.Root, filepath.FromSlash(operation.Path)))
		if err == nil && bytes.Equal(current, operation.Content) {
			plan.SyncedPaths = append(plan.SyncedPaths, operation.Path)
		} else if err == nil || errors.Is(err, os.ErrNotExist) {
			plan.ChangedPaths = append(plan.ChangedPaths, operation.Path)
		} else {
			return Plan{}, fmt.Errorf("inspect planned target %s: %w", operation.Path, err)
		}
	}
	return plan, nil
}

func buildSwiftUIPlan(project Project) (Plan, error) {
	for _, path := range []string{ConfigPath, ".attribution/.gitignore", SwiftSourcePath, SwiftPlistPath, SwiftGuidePath, ManifestPath, project.SwiftUI.ProjectFile} {
		if err := validateSafeTarget(project.Root, path); err != nil {
			return Plan{}, err
		}
	}
	config, rawConfig, err := ReadConfig(project.Root)
	if err != nil {
		return Plan{}, err
	}
	if config.App.BundleID != project.BundleID {
		return Plan{}, &ConfigValidationError{Problems: []string{fmt.Sprintf("app.bundleId %q does not match Xcode target PRODUCT_BUNDLE_IDENTIFIER %q", config.App.BundleID, project.BundleID)}}
	}
	if config.Mode != "managed" || config.ConversionAuthority.Owner != "managed-runtime" {
		return Plan{}, &ConfigValidationError{Problems: []string{"SwiftUI preview setup supports only managed mode with conversionAuthority.owner managed-runtime; native third-party conversion managers remain human-owned"}}
	}
	if config.Providers.Meta != nil {
		return Plan{}, &ConfigValidationError{Problems: []string{"providers.meta is not compiled for SwiftUI hosts in this preview; configure native provider SDKs independently"}}
	}

	configDigest := sha256Hex(rawConfig)
	schemaDigest := schemaHash(config)
	source := renderSwiftSource(config, schemaDigest)
	plist, err := renderSwiftPlist(config, schemaDigest)
	if err != nil {
		return Plan{}, err
	}
	guide := renderSwiftGuide(project)
	manifest := GeneratedManifest{
		Version:        1,
		GeneratedBy:    "attribution " + Version,
		Host:           "swiftui",
		PackageManager: "swiftpm",
		Mode:           config.Mode,
		ConfigHash:     configDigest,
		SchemaHash:     schemaDigest,
		GeneratedFiles: []GeneratedFile{
			{Path: SwiftSourcePath, SHA256: sha256Hex(source)},
			{Path: SwiftPlistPath, SHA256: sha256Hex(plist)},
			{Path: SwiftGuidePath, SHA256: sha256Hex(guide)},
		},
		AppConfig: GeneratedAppConfig{
			Path:           project.SwiftUI.ProjectFile,
			Target:         project.SwiftUI.TargetName,
			InfoPlist:      project.SwiftUI.InfoPlistPath,
			GeneratedSwift: SwiftSourcePath,
			PackageProduct: "AttributionCore",
		},
	}
	manifestRaw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return Plan{}, fmt.Errorf("encode manifest: %w", err)
	}
	manifestRaw = append(manifestRaw, '\n')
	operations := []Operation{
		{Path: ".attribution/.gitignore", Content: []byte("last-run.json\nprobe.json\n")},
		{Path: SwiftSourcePath, Content: source},
		{Path: SwiftPlistPath, Content: plist},
		{Path: SwiftGuidePath, Content: guide},
		{Path: ManifestPath, Content: manifestRaw},
	}
	plan := Plan{Project: project, Config: config, ConfigHash: configDigest, SchemaHash: schemaDigest, Operations: operations}
	for _, operation := range operations {
		current, readErr := os.ReadFile(filepath.Join(project.Root, filepath.FromSlash(operation.Path)))
		if readErr == nil && bytes.Equal(current, operation.Content) {
			plan.SyncedPaths = append(plan.SyncedPaths, operation.Path)
		} else if readErr == nil || errors.Is(readErr, os.ErrNotExist) {
			plan.ChangedPaths = append(plan.ChangedPaths, operation.Path)
		} else {
			return Plan{}, fmt.Errorf("inspect planned target %s: %w", operation.Path, readErr)
		}
	}
	return plan, nil
}

func renderSwiftSource(config Config, schemaDigest string) []byte {
	var eventPairs []string
	for index, event := range config.Schema.Events {
		eventPairs = append(eventPairs, fmt.Sprintf("        %s: %d", quoteSwift(event), index))
	}
	source := `// Generated by attribution — do not edit. Regenerate with ` + "`attribution apply`" + `.
import AttributionCore
import Foundation

enum AttributionKitGeneratedPlan {
    static let schemaHash = ` + quoteSwift(schemaDigest) + `
    static let eventValues: [String: Int] = [
` + strings.Join(eventPairs, ",\n") + `
    ]

    static func configuration(bundle: Bundle = .main) throws -> AttributionConfiguration {
        let observed = try AttributionConfiguration.fromBundle(bundle)
        guard observed.schemaHash == schemaHash, observed.eventValues == eventValues else {
            throw AttributionCoreError.invalidBundleConfiguration("generated-plan")
        }
        return observed
    }

    static func record(_ event: String, bundle: Bundle = .main) async throws -> AttributionUpdateReport {
        try await AttributionCore.record(event, configuration: configuration(bundle: bundle))
    }
}
`
	return []byte(source)
}

func quoteSwift(value string) string {
	raw, _ := json.Marshal(value)
	return string(raw)
}

func renderSwiftPlist(config Config, schemaDigest string) ([]byte, error) {
	endpoint, err := normalizedEndpoint(config.Providers.Apple.Endpoint)
	if err != nil {
		return nil, err
	}
	var body strings.Builder
	body.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>NSAdvertisingAttributionReportEndpoint</key>
  <string>`)
	body.WriteString(escapeXML(endpoint))
	body.WriteString(`</string>
  <key>SKAdNetworkItems</key>
  <array>
`)
	for _, id := range config.Providers.Apple.SKAdNetworkIDs {
		body.WriteString("    <dict>\n      <key>SKAdNetworkIdentifier</key>\n      <string>")
		body.WriteString(escapeXML(id))
		body.WriteString("</string>\n    </dict>\n")
	}
	body.WriteString(`  </array>
  <key>AttributionKitSchemaHash</key>
  <string>`)
	body.WriteString(schemaDigest)
	body.WriteString(`</string>
  <key>AttributionKitEventValues</key>
  <dict>
`)
	for index, event := range config.Schema.Events {
		body.WriteString("    <key>")
		body.WriteString(escapeXML(event))
		body.WriteString("</key>\n    <integer>")
		body.WriteString(fmt.Sprintf("%d", index))
		body.WriteString("</integer>\n")
	}
	body.WriteString("  </dict>\n</dict>\n</plist>\n")
	return []byte(body.String()), nil
}

func escapeXML(value string) string {
	var output bytes.Buffer
	_ = xml.EscapeText(&output, []byte(value))
	return output.String()
}

func normalizedEndpoint(value string) (string, error) {
	endpoint, err := url.Parse(value)
	if err != nil {
		return "", err
	}
	endpoint.Path = "/"
	endpoint.RawPath = ""
	endpoint.RawQuery = ""
	endpoint.Fragment = ""
	return endpoint.String(), nil
}

func renderSwiftGuide(project Project) []byte {
	infoInstruction := "Create an explicit project-relative Info.plist for every target configuration and copy the four generated keys from `" + SwiftPlistPath + "`."
	if project.SwiftUI.InfoPlistPath != "" {
		infoInstruction = "Copy the four generated keys from `" + SwiftPlistPath + "` into `" + project.SwiftUI.InfoPlistPath + "` without removing unrelated app keys."
	}
	return []byte("# AttributionKit Xcode integration (generated)\n\n" +
		"Target: `" + project.SwiftUI.TargetName + "` in `" + project.SwiftUI.XcodeProject + "`\n\n" +
		"1. Add `" + AttributionRepo + "` as a Swift package and link product `AttributionCore` to this application target.\n" +
		"2. Add `" + SwiftSourcePath + "` to this target's Sources build phase. Do not copy or rename it.\n" +
		"3. " + infoInstruction + "\n" +
		"4. Call `AttributionKitGeneratedPlan.record(\"install\")` from the app.\n" +
		"5. Run `attribution verify --json`; generated files alone are intentionally insufficient.\n")
}

func renderWrapper(config Config, schemaDigest string) ([]byte, error) {
	endpoint, err := url.Parse(config.Providers.Apple.Endpoint)
	if err != nil {
		return nil, err
	}
	endpoint.Path = "/"
	endpoint.RawPath = ""
	endpoint.RawQuery = ""
	endpoint.Fragment = ""

	type options struct {
		Endpoint                       string   `json:"endpoint"`
		SKAdNetworkIDs                 []string `json:"skAdNetworkIds"`
		Events                         []string `json:"events"`
		SchemaHash                     string   `json:"schemaHash"`
		MetaAppID                      *string  `json:"metaAppId,omitempty"`
		DisableMetaConversionReporting bool     `json:"disableMetaConversionReporting"`
	}
	values := options{
		Endpoint:                       endpoint.String(),
		SKAdNetworkIDs:                 config.Providers.Apple.SKAdNetworkIDs,
		Events:                         config.Schema.Events,
		SchemaHash:                     schemaDigest,
		DisableMetaConversionReporting: shouldDisableMetaConversion(config),
	}
	if config.Providers.Meta != nil {
		values.MetaAppID = &config.Providers.Meta.AppID
	}
	raw, err := json.MarshalIndent(values, "", "  ")
	if err != nil {
		return nil, err
	}
	source := `// Generated by attribution — do not edit. Regenerate with ` + "`attribution apply`" + `.
// This wrapper keeps attribution configuration deterministic across expo prebuild --clean.
const withAttribution = require("` + AttributionEntry + `");

const options = ` + string(raw) + `;

module.exports = function withAttributionConfigured(config) {
  return withAttribution(config, options);
};
`
	return []byte(source), nil
}

func Apply(plan Plan, allowDirty bool) (ApplyResult, error) {
	result := ApplyResult{Skipped: append([]string(nil), plan.SyncedPaths...)}
	// Revalidate every owned target even for a no-op plan. A symlink must not
	// become an "in sync" shortcut around the integrity boundary.
	for _, operation := range plan.Operations {
		if err := validateSafeTarget(plan.Project.Root, operation.Path); err != nil {
			return ApplyResult{}, err
		}
	}
	if len(plan.ChangedPaths) == 0 {
		return result, nil
	}
	if !allowDirty {
		dirty, err := unrelatedDirtyPaths(plan)
		if err != nil {
			return ApplyResult{}, err
		}
		if len(dirty) > 0 {
			return ApplyResult{}, &DirtyWorkingTreeError{Paths: dirty}
		}
	}

	changedSet := make(map[string]struct{}, len(plan.ChangedPaths))
	for _, path := range plan.ChangedPaths {
		changedSet[path] = struct{}{}
	}
	for _, operation := range plan.Operations {
		if _, changed := changedSet[operation.Path]; !changed {
			continue
		}
		if err := safeAtomicWrite(plan.Project.Root, operation.Path, operation.Content); err != nil {
			return ApplyResult{}, err
		}
		result.Changed = append(result.Changed, operation.Path)
	}
	return result, nil
}

func validateSafeTarget(root, relative string) error {
	clean := filepath.Clean(filepath.FromSlash(relative))
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("refusing unsafe output path %q", relative)
	}
	current := root
	parts := strings.Split(clean, string(filepath.Separator))
	for _, part := range parts {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("inspect output path %s: %w", relative, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to write %s through symlink %s", relative, current)
		}
	}
	return nil
}

func safeAtomicWrite(root, relative string, content []byte) error {
	if err := validateSafeTarget(root, relative); err != nil {
		return err
	}
	target := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("create parent for %s: %w", relative, err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(target), ".attribution-write-*")
	if err != nil {
		return fmt.Errorf("create temporary file for %s: %w", relative, err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o644); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		temporary.Close()
		return fmt.Errorf("write %s: %w", relative, err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync %s: %w", relative, err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close %s: %w", relative, err)
	}
	if err := os.Rename(temporaryName, target); err != nil {
		return fmt.Errorf("replace %s: %w", relative, err)
	}
	return nil
}

func unrelatedDirtyPaths(plan Plan) ([]string, error) {
	inside := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	inside.Dir = plan.Project.Root
	if err := inside.Run(); err != nil {
		return nil, nil
	}
	paths := make(map[string]struct{})
	commands := [][]string{
		{"diff", "--name-only"},
		{"diff", "--cached", "--name-only"},
		{"ls-files", "--others", "--exclude-standard"},
	}
	for _, args := range commands {
		command := exec.Command("git", args...)
		command.Dir = plan.Project.Root
		output, err := command.Output()
		if err != nil {
			return nil, fmt.Errorf("inspect git working tree: %w", err)
		}
		for _, line := range strings.Split(string(output), "\n") {
			line = filepath.ToSlash(strings.TrimSpace(line))
			if line != "" {
				paths[line] = struct{}{}
			}
		}
	}
	changed := make(map[string]struct{}, len(plan.ChangedPaths))
	for _, path := range plan.ChangedPaths {
		changed[path] = struct{}{}
	}
	var unrelated []string
	for path := range paths {
		if path == ".attribution" || strings.HasPrefix(path, ".attribution/") {
			continue
		}
		// app.json can be dirty solely because a previous apply registered the
		// plugin. It is safe only when this plan will not write it again.
		if path == "app.json" {
			if _, willChange := changed[path]; !willChange {
				continue
			}
		}
		unrelated = append(unrelated, path)
	}
	sort.Strings(unrelated)
	return unrelated, nil
}

func CreateBranch(root, name string) error {
	if name == "" {
		name = "attribution/setup"
	}
	command := exec.Command("git", "checkout", "-b", name)
	command.Dir = root
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("create branch %q: %w: %s", name, err, strings.TrimSpace(string(output)))
	}
	return nil
}

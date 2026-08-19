package attribution

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

type CheckResult struct {
	CheckID          string `json:"checkId"`
	RuleVersion      string `json:"ruleVersion"`
	Section          string `json:"section"`
	Execution        string `json:"execution"`
	Verdict          string `json:"verdict"`
	Evidence         string `json:"evidence"`
	Basis            string `json:"basis"`
	Integrity        string `json:"integrity"`
	Comparability    string `json:"comparability"`
	CollectionHealth string `json:"collectionHealth"`
	Finality         string `json:"finality"`
	Reason           string `json:"reason"`
	Remediation      string `json:"remediation,omitempty"`
}

type EnvironmentAttributes struct {
	Distribution        string  `json:"distribution"`
	Protocol            string  `json:"protocol"`
	TestMechanism       string  `json:"testMechanism"`
	PurchaseEnvironment string  `json:"purchaseEnvironment"`
	SigningKey          string  `json:"signingKey"`
	Revision            *string `json:"revision"`
}

type ProjectIdentity struct {
	BundleID   *string `json:"bundleId"`
	ConfigHash *string `json:"configHash"`
	SchemaHash *string `json:"schemaHash"`
}

type RunManifest struct {
	RunID         string                `json:"runId"`
	SchemaVersion string                `json:"schemaVersion"`
	StartedAt     string                `json:"startedAt"`
	FinishedAt    string                `json:"finishedAt"`
	Environment   EnvironmentAttributes `json:"environment"`
	Project       ProjectIdentity       `json:"project"`
	Results       []CheckResult         `json:"results"`
}

type RunEvent struct {
	Type          string       `json:"type"`
	RunID         string       `json:"runId,omitempty"`
	SchemaVersion string       `json:"schemaVersion,omitempty"`
	CheckID       string       `json:"checkId,omitempty"`
	Result        *CheckResult `json:"result,omitempty"`
	Manifest      *RunManifest `json:"manifest,omitempty"`
}

type VerifyResult struct {
	Manifest      RunManifest
	PersistedPath string
}

type EmitFunc func(RunEvent) error

type observation struct {
	project       Project
	config        *Config
	configRaw     []byte
	configError   string
	pluginRaw     []byte
	pluginMissing bool
	generatedRaw  map[string][]byte
	swift         SwiftUIIntegration
	swiftInfo     map[string]any
	swiftInfoErr  string
	manifest      *GeneratedManifest
	manifestRaw   []byte
	manifestError string
	probe         *RuntimeProbeArtifact
	probeError    string
	secretHits    []string
}

type rule struct {
	id            string
	version       string
	section       string
	evidence      string
	integrity     string
	comparability string
	evaluate      func(observation) CheckResult
}

var secretPatterns = []struct {
	name    string
	pattern *regexp.Regexp
}{
	{name: "Stripe live secret", pattern: regexp.MustCompile(`(?:sk|rk)_live_[0-9A-Za-z]{8,}`)},
	{name: "AWS access key", pattern: regexp.MustCompile(`(?:AKIA|ASIA)[0-9A-Z]{16}`)},
	{name: "private key", pattern: regexp.MustCompile(`-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----`)},
	{name: "GitHub token", pattern: regexp.MustCompile(`(?:gh[pousr]_[0-9A-Za-z]{20,}|github_pat_[0-9A-Za-z_]{20,})`)},
	{name: "Slack token", pattern: regexp.MustCompile(`xox[baprs]-[0-9A-Za-z-]{10,}`)},
	{name: "generic client secret", pattern: regexp.MustCompile(`(?i)(?:api[_-]?key|client[_-]?secret|access[_-]?token|refresh[_-]?token)\s*[:=]\s*["'][0-9A-Za-z_./+:=-]{20,}["']`)},
}

func RunVerify(root string, emit EmitFunc) (VerifyResult, error) {
	if emit == nil {
		emit = func(RunEvent) error { return nil }
	}
	runID, err := randomID()
	if err != nil {
		return VerifyResult{}, fmt.Errorf("create run id: %w", err)
	}
	started := time.Now().UTC()
	if err := emit(RunEvent{Type: "run_started", RunID: runID, SchemaVersion: SchemaVersion}); err != nil {
		return VerifyResult{}, err
	}

	obs, collectErr := collectObservation(root)
	rules := verificationRules("expo")
	if collectErr == nil {
		rules = verificationRules(obs.project.Host)
	}
	results := make([]CheckResult, 0, len(rules))
	for _, currentRule := range rules {
		if err := emit(RunEvent{Type: "check_started", CheckID: currentRule.id}); err != nil {
			return VerifyResult{}, err
		}
		var result CheckResult
		if collectErr != nil {
			result = CheckResult{
				CheckID: currentRule.id, RuleVersion: currentRule.version,
				Section: currentRule.section, Execution: "error", Verdict: "unknown",
				Evidence: currentRule.evidence, Basis: "unknown", Integrity: "unknown", Comparability: "none",
				Reason: "collector failed: " + collectErr.Error(),
			}
		} else {
			result = evaluateRuleSafely(currentRule, obs)
		}
		if result.CollectionHealth == "" {
			result.CollectionHealth = "unknown"
		}
		if result.Finality == "" {
			result.Finality = "settled"
			if result.Section == "your-logic" || result.Section == "device" || result.Section == "production" {
				result.Finality = "provisional"
			}
		}
		results = append(results, result)
		copy := result
		if err := emit(RunEvent{Type: "check_completed", Result: &copy}); err != nil {
			return VerifyResult{}, err
		}
	}

	protocol := "unknown"
	projectIdentity := ProjectIdentity{}
	if collectErr == nil && obs.config != nil {
		bundleID := obs.config.App.BundleID
		configDigest := sha256Hex(obs.configRaw)
		schemaDigest := schemaHash(*obs.config)
		projectIdentity = ProjectIdentity{BundleID: &bundleID, ConfigHash: &configDigest, SchemaHash: &schemaDigest}
		if len(obs.config.Providers.Apple.SKAdNetworkIDs) > 0 {
			protocol = "both"
		} else {
			protocol = "aak"
		}
	}
	testMechanism := "none"
	for _, result := range results {
		if result.CheckID == "runtime.report-imported" && result.Execution == "succeeded" && result.Verdict == "pass" {
			testMechanism = "simulator"
			break
		}
	}
	manifest := RunManifest{
		RunID: runID, SchemaVersion: SchemaVersion,
		StartedAt: started.Format(time.RFC3339Nano), FinishedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Environment: EnvironmentAttributes{
			Distribution: "dev", Protocol: protocol, TestMechanism: testMechanism,
			PurchaseEnvironment: "none", SigningKey: "unknown", Revision: gitRevision(root),
		},
		Project: projectIdentity,
		Results: results,
	}
	if err := validateRunManifest(manifest); err != nil {
		return VerifyResult{}, fmt.Errorf("internal run manifest validation failed: %w", err)
	}
	manifestCopy := manifest
	if err := emit(RunEvent{Type: "run_completed", Manifest: &manifestCopy}); err != nil {
		return VerifyResult{}, err
	}

	result := VerifyResult{Manifest: manifest}
	attributeDir := filepath.Join(root, ".attribution")
	if info, err := os.Lstat(attributeDir); err == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
		raw, err := json.MarshalIndent(manifest, "", "  ")
		if err != nil {
			return VerifyResult{}, err
		}
		if err := safeAtomicWrite(root, LastRunPath, append(raw, '\n')); err != nil {
			return VerifyResult{}, fmt.Errorf("persist run manifest: %w", err)
		}
		result.PersistedPath = LastRunPath
	}
	return result, nil
}

func randomID() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	// UUIDv4 formatting is sufficient for local run identity and needs no
	// network, clock sequence, or third-party package.
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	hexValue := hex.EncodeToString(raw)
	return hexValue[0:8] + "-" + hexValue[8:12] + "-" + hexValue[12:16] + "-" + hexValue[16:20] + "-" + hexValue[20:32], nil
}

func collectObservation(root string) (observation, error) {
	project, err := DiscoverProject(root)
	if err != nil {
		return observation{}, err
	}
	paths := []string{ConfigPath, ".attribution/.gitignore", ManifestPath, ProbePath}
	if project.Host == "swiftui" {
		paths = append(paths, SwiftSourcePath, SwiftPlistPath, SwiftGuidePath, project.SwiftUI.ProjectFile)
		if project.SwiftUI.InfoPlistPath != "" {
			paths = append(paths, project.SwiftUI.InfoPlistPath)
		}
	} else {
		paths = append(paths, PluginPath, ExpoFacadePath)
	}
	for _, path := range paths {
		if err := validateSafeTarget(project.Root, path); err != nil {
			return observation{}, err
		}
	}
	obs := observation{project: project, generatedRaw: map[string][]byte{}}
	config, raw, err := ReadConfig(project.Root)
	if err != nil {
		obs.configError = err.Error()
	} else {
		obs.config = &config
		obs.configRaw = raw
	}
	generatedPaths := []string{PluginPath, ExpoFacadePath}
	if project.Host == "swiftui" {
		generatedPaths = []string{SwiftSourcePath, SwiftPlistPath, SwiftGuidePath}
		obs.swift = inspectSwiftUIIntegration(project)
		if info, infoErr := loadSwiftInfoPlist(project); infoErr != nil {
			obs.swiftInfoErr = infoErr.Error()
		} else {
			obs.swiftInfo = info
		}
	}
	for _, generatedPath := range generatedPaths {
		generated, readErr := os.ReadFile(filepath.Join(project.Root, filepath.FromSlash(generatedPath)))
		if errors.Is(readErr, os.ErrNotExist) {
			if generatedPath == PluginPath || generatedPath == SwiftSourcePath {
				obs.pluginMissing = true
			}
			continue
		}
		if readErr != nil {
			return observation{}, fmt.Errorf("read %s: %w", generatedPath, readErr)
		}
		obs.generatedRaw[generatedPath] = generated
		if generatedPath == PluginPath || generatedPath == SwiftSourcePath {
			obs.pluginRaw = generated
		}
	}
	manifestRaw, err := os.ReadFile(filepath.Join(project.Root, filepath.FromSlash(ManifestPath)))
	if errors.Is(err, os.ErrNotExist) {
		obs.manifestError = ManifestPath + " not found"
	} else if err != nil {
		return observation{}, fmt.Errorf("read %s: %w", ManifestPath, err)
	} else {
		var manifest GeneratedManifest
		decoder := json.NewDecoder(bytes.NewReader(manifestRaw))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&manifest); err != nil {
			obs.manifestError = "invalid " + ManifestPath + ": " + err.Error()
		} else if err := decoder.Decode(&struct{}{}); err == nil {
			obs.manifestError = "invalid " + ManifestPath + ": multiple JSON values"
		} else if !errors.Is(err, io.EOF) {
			obs.manifestError = "invalid " + ManifestPath + ": " + err.Error()
		} else {
			obs.manifest = &manifest
			obs.manifestRaw = manifestRaw
		}
	}
	obs.probe, obs.probeError = loadRuntimeProbe(project.Root)
	hits, err := scanSecretShapedValues(project.Root)
	if err != nil {
		return observation{}, err
	}
	obs.secretHits = hits
	return obs, nil
}

func verificationRules(host string) []rule {
	if host == "swiftui" {
		return []rule{
			{id: "schema.valid", version: "1.0.0", section: "config", evidence: "static", integrity: "observed_static", comparability: "none", evaluate: ruleSchemaValid},
			{id: "swiftui.package-linked", version: "1.0.0", section: "build", evidence: "static", integrity: "observed_static", comparability: "none", evaluate: ruleSwiftPackageLinked},
			{id: "swiftui.generated-source-targeted", version: "1.0.0", section: "build", evidence: "static", integrity: "generated", comparability: "none", evaluate: ruleSwiftSourceTargeted},
			{id: "swiftui.info-plist-plan", version: "1.0.0", section: "build", evidence: "static", integrity: "observed_static", comparability: "exact", evaluate: ruleSwiftInfoPlan},
			{id: "app.bundle-id-matches", version: "1.0.0", section: "config", evidence: "static", integrity: "observed_static", comparability: "none", evaluate: ruleBundleID},
			{id: "apple.conversion-authority.single-owner", version: "1.0.0", section: "config", evidence: "static", integrity: "observed_static", comparability: "none", evaluate: ruleSingleOwner},
			{id: "apple.endpoint.report-attribution", version: "1.0.0", section: "config", evidence: "static", integrity: "observed_static", comparability: "none", evaluate: ruleEndpoint},
			{id: "apple.skan.items-present", version: "1.0.0", section: "config", evidence: "static", integrity: "observed_static", comparability: "none", evaluate: ruleSKAdItems},
			{id: "meta.app-id-wired", version: "1.0.0", section: "config", evidence: "static", integrity: "observed_static", comparability: "none", evaluate: ruleMetaAppID},
			{id: "meta.conversion-management-disabled", version: "1.0.0", section: "config", evidence: "static", integrity: "observed_static", comparability: "none", evaluate: ruleMetaConversion},
			{id: "secrets.none-in-client", version: "1.0.0", section: "build", evidence: "static", integrity: "observed_static", comparability: "none", evaluate: ruleNoSecrets},
			{id: "generated.manifest-hashes", version: "1.0.0", section: "build", evidence: "static", integrity: "generated", comparability: "none", evaluate: ruleManifestHashes},
			{id: "runtime.report-imported", version: "1.0.0", section: "your-logic", evidence: "simulator", integrity: "unknown", comparability: "none", evaluate: ruleRuntimeProbe},
			{id: "device.aak-postback", version: "1.0.0", section: "device", evidence: "device-lab", integrity: "unknown", comparability: "none", evaluate: ruleDevicePending},
			{id: "production.winning-copy", version: "1.0.0", section: "production", evidence: "apple", integrity: "unknown", comparability: "none", evaluate: ruleProductionPending},
		}
	}
	return []rule{
		{id: "schema.valid", version: "1.0.0", section: "config", evidence: "static", integrity: "observed_static", comparability: "none", evaluate: ruleSchemaValid},
		{id: "expo.package-installed", version: "1.0.0", section: "config", evidence: "static", integrity: "observed_static", comparability: "none", evaluate: ruleExpoPackage},
		{id: "expo.plugin-registered", version: "1.0.0", section: "config", evidence: "static", integrity: "observed_static", comparability: "none", evaluate: rulePluginRegistered},
		{id: "expo.plugin-wrapper", version: "1.0.0", section: "build", evidence: "static", integrity: "generated", comparability: "none", evaluate: rulePluginWrapper},
		{id: "app.bundle-id-matches", version: "1.0.0", section: "config", evidence: "static", integrity: "observed_static", comparability: "none", evaluate: ruleBundleID},
		{id: "apple.conversion-authority.single-owner", version: "1.0.0", section: "config", evidence: "static", integrity: "observed_static", comparability: "none", evaluate: ruleSingleOwner},
		{id: "apple.endpoint.report-attribution", version: "1.0.0", section: "config", evidence: "static", integrity: "generated", comparability: "none", evaluate: ruleEndpoint},
		{id: "apple.skan.items-present", version: "1.0.0", section: "config", evidence: "static", integrity: "generated", comparability: "none", evaluate: ruleSKAdItems},
		{id: "meta.app-id-wired", version: "1.0.0", section: "config", evidence: "static", integrity: "generated", comparability: "none", evaluate: ruleMetaAppID},
		{id: "meta.conversion-management-disabled", version: "1.0.0", section: "config", evidence: "static", integrity: "generated", comparability: "none", evaluate: ruleMetaConversion},
		{id: "secrets.none-in-client", version: "1.0.0", section: "build", evidence: "static", integrity: "observed_static", comparability: "none", evaluate: ruleNoSecrets},
		{id: "generated.manifest-hashes", version: "1.0.0", section: "build", evidence: "static", integrity: "generated", comparability: "none", evaluate: ruleManifestHashes},
		{id: "runtime.report-imported", version: "1.0.0", section: "your-logic", evidence: "simulator", integrity: "unknown", comparability: "none", evaluate: ruleRuntimeProbe},
		{id: "device.aak-postback", version: "1.0.0", section: "device", evidence: "device-lab", integrity: "unknown", comparability: "none", evaluate: ruleDevicePending},
		{id: "production.winning-copy", version: "1.0.0", section: "production", evidence: "apple", integrity: "unknown", comparability: "none", evaluate: ruleProductionPending},
	}
}

func evaluateRuleSafely(current rule, obs observation) (result CheckResult) {
	result = CheckResult{
		CheckID: current.id, RuleVersion: current.version, Section: current.section,
		Execution: "succeeded", Verdict: "unknown", Evidence: current.evidence,
		Basis: "unknown", Integrity: current.integrity, Comparability: current.comparability, Reason: "rule returned no result",
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			result = CheckResult{
				CheckID: current.id, RuleVersion: current.version, Section: current.section,
				Execution: "error", Verdict: "unknown", Evidence: current.evidence,
				Basis: "unknown", Integrity: "unknown", Comparability: "none", Reason: fmt.Sprintf("rule panicked: %v", recovered),
			}
		}
	}()
	evaluated := current.evaluate(obs)
	evaluated.CheckID = current.id
	evaluated.RuleVersion = current.version
	evaluated.Section = current.section
	evaluated.Evidence = current.evidence
	if evaluated.Execution == "" {
		evaluated.Execution = "succeeded"
	}
	if evaluated.Basis == "" {
		evaluated.Basis = "unknown"
	}
	if evaluated.Integrity == "" {
		evaluated.Integrity = current.integrity
	}
	if evaluated.Comparability == "" {
		evaluated.Comparability = current.comparability
	}
	return evaluated
}

func validConfigOrUnknown(obs observation, subject string) *CheckResult {
	if obs.config != nil {
		return nil
	}
	return &CheckResult{Verdict: "unknown", Reason: "cannot evaluate " + subject + ": " + obs.configError}
}

func ruleSchemaValid(obs observation) CheckResult {
	if obs.config == nil {
		remediation := "Fix " + ConfigPath + " to match the published schema."
		if strings.Contains(obs.configError, "run `attribution init`") {
			remediation = "Run `attribution init` to create the desired-state file."
		}
		return CheckResult{Verdict: "fail", Reason: "desired state invalid: " + obs.configError, Remediation: remediation}
	}
	return CheckResult{Verdict: "pass", Reason: "desired state parses; events are known and unique, and authority is separate from transports"}
}

func ruleExpoPackage(obs observation) CheckResult {
	if !attributionPackageInstalled(obs.project) {
		return CheckResult{Verdict: "fail", Reason: AttributionPkg + " and its app.plugin.js entrypoint are not locally installed/resolvable", Remediation: "Install it with `" + installCommand(obs.project.PackageManager) + "`."}
	}
	return CheckResult{Verdict: "pass", Reason: AttributionPkg + " and its app.plugin.js entrypoint are locally installed/resolvable"}
}

func rulePluginRegistered(obs observation) CheckResult {
	if legacyPluginRegistered(obs.project.AppJSON) || conflictingAttributionPluginRegistered(obs.project.AppJSON) {
		return CheckResult{Verdict: "fail", Reason: "app.json contains a legacy, direct, duplicate, or non-final Attribution plugin registration", Remediation: "Run `attribution apply` to leave exactly one ./" + PluginPath + " registration in final position."}
	}
	if !pluginRegistered(obs.project.AppJSON) {
		return CheckResult{Verdict: "fail", Reason: "app.json does not register exactly one final-position ./" + PluginPath, Remediation: "Run `attribution apply`."}
	}
	return CheckResult{Verdict: "pass", Reason: "app.json registers exactly one ./" + PluginPath + " in final position"}
}

func rulePluginWrapper(obs observation) CheckResult {
	if unknown := validConfigOrUnknown(obs, "the generated Expo wrapper"); unknown != nil {
		return *unknown
	}
	if obs.pluginMissing {
		return CheckResult{Verdict: "fail", Reason: "generated Expo wrapper is missing", Remediation: "Run `attribution apply`."}
	}
	expected, err := renderWrapper(*obs.config, schemaHash(*obs.config), releaseManifestForProject(obs.project.Root, *obs.config, sha256Hex(obs.configRaw), schemaHash(*obs.config)))
	if err != nil {
		panic(err)
	}
	if !bytes.Equal(obs.pluginRaw, expected) {
		return CheckResult{Verdict: "fail", Reason: "generated Expo wrapper drifted from desired state or does not invoke " + AttributionEntry, Remediation: "Run `attribution apply` to regenerate it."}
	}
	return CheckResult{Verdict: "pass", Reason: "deterministic wrapper invokes " + AttributionEntry + " with the expected options"}
}

func ruleSwiftPackageLinked(obs observation) CheckResult {
	if !obs.swift.PackageLinked {
		return CheckResult{Verdict: "fail", Reason: obs.swift.PackageProblem, Remediation: "In Xcode, add " + AttributionRepo + " and link its AttributionCore product to target " + obs.project.SwiftUI.TargetName + "."}
	}
	return CheckResult{Verdict: "pass", Reason: "the application target links AttributionCore from the official AttributionKit Swift package"}
}

func ruleSwiftSourceTargeted(obs observation) CheckResult {
	if unknown := validConfigOrUnknown(obs, "the generated Swift plan"); unknown != nil {
		return *unknown
	}
	if obs.pluginMissing {
		return CheckResult{Verdict: "fail", Reason: "generated Swift plan is missing", Remediation: "Run `attribution apply`."}
	}
	expected := renderSwiftSource(*obs.config, schemaHash(*obs.config))
	if !bytes.Equal(obs.pluginRaw, expected) {
		return CheckResult{Verdict: "fail", Reason: "generated Swift plan drifted from desired state", Remediation: "Run `attribution apply`."}
	}
	if !obs.swift.SourceTargeted {
		return CheckResult{Verdict: "fail", Reason: obs.swift.SourceProblem, Remediation: "Add " + SwiftSourcePath + " to target " + obs.project.SwiftUI.TargetName + " without copying or renaming it."}
	}
	return CheckResult{Verdict: "pass", Reason: "the exact generated Swift plan is present in the application target's Sources build phase"}
}

func ruleSwiftInfoPlan(obs observation) CheckResult {
	if unknown := validConfigOrUnknown(obs, "the target-declared SwiftUI Info.plist plan"); unknown != nil {
		return *unknown
	}
	if obs.swiftInfo == nil {
		return CheckResult{Verdict: "fail", Reason: "target Info.plist is not inspectable: " + obs.swiftInfoErr, Remediation: "Follow " + SwiftGuidePath + " and declare an explicit XML INFOPLIST_FILE for every target configuration."}
	}
	wantedSchema := schemaHash(*obs.config)
	observedSchema, schemaOK := obs.swiftInfo["AttributionKitSchemaHash"].(string)
	if !schemaOK || observedSchema != wantedSchema {
		return CheckResult{Verdict: "fail", Reason: "target Info.plist AttributionKitSchemaHash does not match the generated plan", Remediation: "Copy the current values from " + SwiftPlistPath + " into " + obs.project.SwiftUI.InfoPlistPath + "."}
	}
	observedEvents, eventsOK := obs.swiftInfo["AttributionKitEventValues"].(map[string]any)
	if !eventsOK || len(observedEvents) != len(obs.config.Schema.Events) {
		return CheckResult{Verdict: "fail", Reason: "target Info.plist AttributionKitEventValues does not exactly match the generated plan", Remediation: "Copy the current values from " + SwiftPlistPath + " into " + obs.project.SwiftUI.InfoPlistPath + "."}
	}
	for index, event := range obs.config.Schema.Events {
		value, ok := observedEvents[event].(int)
		if !ok || value != index {
			return CheckResult{Verdict: "fail", Reason: fmt.Sprintf("target Info.plist event %s does not map to generated conversion value %d", event, index), Remediation: "Copy the current values from " + SwiftPlistPath + " into " + obs.project.SwiftUI.InfoPlistPath + "."}
		}
	}
	return CheckResult{Verdict: "pass", Reason: "target Info.plist schema hash and event-value dictionary exactly match the generated native plan"}
}

func ruleBundleID(obs observation) CheckResult {
	if unknown := validConfigOrUnknown(obs, "the bundle identifier"); unknown != nil {
		return *unknown
	}
	if obs.project.BundleID == "" {
		if obs.project.Host == "swiftui" {
			return CheckResult{Verdict: "fail", Reason: "Xcode target does not declare an explicit PRODUCT_BUNDLE_IDENTIFIER", Remediation: "Set one literal bundle identifier in every target build configuration."}
		}
		return CheckResult{Verdict: "fail", Reason: "app.json does not declare expo.ios.bundleIdentifier", Remediation: "Set expo.ios.bundleIdentifier to the app's real bundle identifier, then run `attribution apply`."}
	}
	if obs.project.BundleID != obs.config.App.BundleID {
		return CheckResult{Verdict: "fail", Reason: fmt.Sprintf("desired bundle id %s does not match app.json bundle id %s", obs.config.App.BundleID, obs.project.BundleID), Remediation: "Make app.bundleId and expo.ios.bundleIdentifier agree."}
	}
	if obs.project.Host == "swiftui" {
		return CheckResult{Verdict: "pass", Reason: "desired bundle id matches the Xcode application target"}
	}
	return CheckResult{Verdict: "pass", Reason: "desired bundle id matches app.json"}
}

func ruleSingleOwner(obs observation) CheckResult {
	if unknown := validConfigOrUnknown(obs, "conversion authority"); unknown != nil {
		return *unknown
	}
	if obs.project.Host == "swiftui" {
		if obs.config.Mode != "managed" || obs.config.ConversionAuthority.Owner != "managed-runtime" {
			return CheckResult{Verdict: "fail", Reason: "SwiftUI host does not have a verifiable managed-runtime single owner", Remediation: "Use managed mode and keep native third-party conversion managers outside this preview setup."}
		}
		return CheckResult{Verdict: "pass", Reason: "desired state assigns conversion updates to the generated AttributionCore call site; native third-party SDK ownership remains outside this preview's automated inspection"}
	}
	installed := installedManagers(obs.project)
	if obs.config.Mode == "external" {
		ownerFound := false
		var competing []string
		for _, manager := range installed {
			if managerMatchesOwner(manager, obs.config.ConversionAuthority.Owner) {
				ownerFound = ownerFound || managerPackageInstalled(obs.project, manager)
			} else if manager.Disableable && shouldDisableMetaConversion(*obs.config) && bytes.Contains(obs.pluginRaw, []byte(`"disableMetaConversionReporting": true`)) {
				continue
			} else {
				competing = append(competing, manager.Name+" ("+manager.Package+")")
			}
		}
		if !ownerFound {
			return CheckResult{Verdict: "fail", Reason: fmt.Sprintf("declared external authority %q does not correspond to an installed known conversion manager", obs.config.ConversionAuthority.Owner), Remediation: "Install and declare the same known manager, or switch to managed mode."}
		}
		if len(competing) > 0 {
			return CheckResult{Verdict: "fail", Reason: "competing conversion-value manager detected: " + strings.Join(competing, ", "), Remediation: "Keep exactly one external conversion authority."}
		}
		return CheckResult{Verdict: "pass", Reason: "declared external authority corresponds to the installed known manager; no known competitor detected"}
	}

	var competing []string
	for _, manager := range installed {
		if manager.Disableable && shouldDisableMetaConversion(*obs.config) && bytes.Contains(obs.pluginRaw, []byte(`"disableMetaConversionReporting": true`)) && bytes.Contains(obs.pluginRaw, []byte(`"metaAppId": "`+obs.config.Providers.Meta.AppID+`"`)) {
			continue
		}
		competing = append(competing, manager.Name+" ("+manager.Package+")")
	}
	if len(competing) > 0 {
		return CheckResult{Verdict: "fail", Reason: "competing conversion-value manager detected in the dependency graph: " + strings.Join(competing, ", "), Remediation: "Remove it or switch to external mode and declare the installed manager as conversionAuthority.owner."}
	}
	return CheckResult{Verdict: "pass", Reason: "no known competing conversion-value manager detected"}
}

func ruleEndpoint(obs observation) CheckResult {
	if unknown := validConfigOrUnknown(obs, "the Apple endpoint"); unknown != nil {
		return *unknown
	}
	if obs.project.Host == "swiftui" {
		if obs.swiftInfo == nil {
			return CheckResult{Verdict: "fail", Reason: "target Info.plist is not inspectable: " + obs.swiftInfoErr, Remediation: "Follow " + SwiftGuidePath + "."}
		}
		expected, _ := normalizedEndpoint(obs.config.Providers.Apple.Endpoint)
		observed, ok := obs.swiftInfo["NSAdvertisingAttributionReportEndpoint"].(string)
		if !ok || observed != expected {
			return CheckResult{Verdict: "fail", Reason: "target Info.plist does not carry the desired HTTPS Apple report-attribution endpoint", Remediation: "Copy the current endpoint from " + SwiftPlistPath + "."}
		}
		return CheckResult{Verdict: "pass", Reason: "target Info.plist carries the desired Apple report-attribution endpoint"}
	}
	expected, _ := renderWrapper(*obs.config, schemaHash(*obs.config), releaseManifestForProject(obs.project.Root, *obs.config, sha256Hex(obs.configRaw), schemaHash(*obs.config)))
	if obs.pluginMissing || !bytes.Equal(obs.pluginRaw, expected) {
		return CheckResult{Verdict: "fail", Reason: "generated wrapper does not carry the desired HTTPS Apple endpoint", Remediation: "Run `attribution apply`."}
	}
	return CheckResult{Verdict: "pass", Reason: "desired Apple report-attribution endpoint is carried by the Expo config plugin"}
}

func ruleSKAdItems(obs observation) CheckResult {
	if unknown := validConfigOrUnknown(obs, "SKAdNetwork ids"); unknown != nil {
		return *unknown
	}
	if len(obs.config.Providers.Apple.SKAdNetworkIDs) == 0 {
		return CheckResult{Verdict: "not_applicable", Reason: "no SKAdNetwork ids are declared in desired state"}
	}
	if obs.project.Host == "swiftui" {
		if obs.swiftInfo == nil {
			return CheckResult{Verdict: "fail", Reason: "target Info.plist is not inspectable: " + obs.swiftInfoErr, Remediation: "Follow " + SwiftGuidePath + "."}
		}
		items, ok := obs.swiftInfo["SKAdNetworkItems"].([]any)
		if !ok {
			return CheckResult{Verdict: "fail", Reason: "target Info.plist SKAdNetworkItems is missing or not an array", Remediation: "Copy the current SKAdNetworkItems from " + SwiftPlistPath + "."}
		}
		observed := map[string]bool{}
		for _, item := range items {
			entry, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if id, ok := entry["SKAdNetworkIdentifier"].(string); ok {
				observed[id] = true
			}
		}
		for _, id := range obs.config.Providers.Apple.SKAdNetworkIDs {
			if !observed[id] {
				return CheckResult{Verdict: "fail", Reason: "target Info.plist is missing declared SKAdNetwork id " + id, Remediation: "Copy the current SKAdNetworkItems from " + SwiftPlistPath + "."}
			}
		}
		return CheckResult{Verdict: "pass", Reason: fmt.Sprintf("%d declared SKAdNetwork id(s) are present in the target Info.plist", len(obs.config.Providers.Apple.SKAdNetworkIDs))}
	}
	for _, id := range obs.config.Providers.Apple.SKAdNetworkIDs {
		if !bytes.Contains(obs.pluginRaw, []byte(id)) {
			return CheckResult{Verdict: "fail", Reason: "generated wrapper is missing declared SKAdNetwork id " + id, Remediation: "Run `attribution apply`."}
		}
	}
	return CheckResult{Verdict: "pass", Reason: fmt.Sprintf("%d declared SKAdNetwork id(s) are carried by the Expo config plugin", len(obs.config.Providers.Apple.SKAdNetworkIDs))}
}

func ruleMetaAppID(obs observation) CheckResult {
	if unknown := validConfigOrUnknown(obs, "the Meta app id"); unknown != nil {
		return *unknown
	}
	if obs.config.Providers.Meta == nil {
		return CheckResult{Verdict: "not_applicable", Reason: "no Meta provider is declared in desired state"}
	}
	if obs.project.Host == "swiftui" {
		return CheckResult{Verdict: "fail", Reason: "providers.meta is declared but native Meta SDK configuration is not compiled by this SwiftUI preview", Remediation: "Remove providers.meta from this managed SwiftUI plan and configure the native SDK independently."}
	}
	if !bytes.Contains(obs.pluginRaw, []byte(`"metaAppId": "`+obs.config.Providers.Meta.AppID+`"`)) {
		return CheckResult{Verdict: "fail", Reason: "generated wrapper does not carry the declared Meta app id", Remediation: "Run `attribution apply`."}
	}
	return CheckResult{Verdict: "pass", Reason: "declared public Meta app id is carried by the Expo config plugin"}
}

func ruleMetaConversion(obs observation) CheckResult {
	if unknown := validConfigOrUnknown(obs, "Meta conversion management"); unknown != nil {
		return *unknown
	}
	if obs.project.Host == "swiftui" {
		return CheckResult{Verdict: "not_applicable", Reason: "native third-party SDK conversion ownership remains outside this preview's automated package inspection"}
	}
	if _, installed := obs.project.Dependencies["react-native-fbsdk-next"]; !installed {
		return CheckResult{Verdict: "not_applicable", Reason: "Meta SDK is not installed; conversion-reporting disablement does not apply"}
	}
	if obs.config.Mode == "external" && managerOwnerInstalled(obs.project, obs.config.ConversionAuthority.Owner, "react-native-fbsdk-next") {
		if obs.config.Providers.Meta == nil {
			return CheckResult{Verdict: "fail", Reason: "Meta is the external authority but providers.meta.appId is absent, so conversion reporting cannot be explicitly enabled", Remediation: "Declare providers.meta.appId with the real public Meta app id, then run `attribution apply`."}
		}
		if !bytes.Contains(obs.pluginRaw, []byte(`"disableMetaConversionReporting": false`)) {
			return CheckResult{Verdict: "fail", Reason: "Meta is the external authority but the generated wrapper does not explicitly keep its conversion reporting enabled", Remediation: "Run `attribution apply`."}
		}
		return CheckResult{Verdict: "pass", Reason: "external Meta authority is compiled with disableMetaConversionReporting=false"}
	}
	if obs.config.Providers.Meta == nil {
		return CheckResult{Verdict: "fail", Reason: "Meta SDK is installed but providers.meta.appId is not declared, so its conversion reporting cannot be proven disabled", Remediation: "Declare providers.meta.appId with the real public Meta app id, then run `attribution apply`."}
	}
	if !bytes.Contains(obs.pluginRaw, []byte(`"disableMetaConversionReporting": true`)) {
		return CheckResult{Verdict: "fail", Reason: "disableMetaConversionReporting=true is absent, so secondary Meta conversion reporting is not proven disabled", Remediation: "Run `attribution apply`, or declare Meta as the external authority."}
	}
	return CheckResult{Verdict: "pass", Reason: "disableMetaConversionReporting=true explicitly demotes Meta to an event transport"}
}

func managerOwnerInstalled(project Project, owner, packageName string) bool {
	for _, manager := range installedManagers(project) {
		if manager.Package == packageName && managerMatchesOwner(manager, owner) {
			return true
		}
	}
	return false
}

func ruleNoSecrets(obs observation) CheckResult {
	if len(obs.secretHits) > 0 {
		return CheckResult{Verdict: "fail", Reason: "secret-shaped values found in client source or tracked files: " + strings.Join(obs.secretHits, "; "), Remediation: "Remove secrets from the shipped client and rotate any exposed credentials."}
	}
	return CheckResult{Verdict: "pass", Reason: "no secret-shaped values found in client source or tracked files"}
}

func ruleManifestHashes(obs observation) CheckResult {
	if unknown := validConfigOrUnknown(obs, "generated hashes"); unknown != nil {
		return *unknown
	}
	if obs.manifest == nil {
		return CheckResult{Verdict: "fail", Reason: obs.manifestError, Remediation: "Run `attribution apply`."}
	}
	wantedConfig := sha256Hex(obs.configRaw)
	wantedSchema := schemaHash(*obs.config)
	if obs.project.Host == "swiftui" {
		if obs.manifest.Version != 1 || obs.manifest.GeneratedBy != "attribution "+Version || obs.manifest.Host != "swiftui" || obs.manifest.Mode != obs.config.Mode || obs.manifest.ConfigHash != wantedConfig || obs.manifest.SchemaHash != wantedSchema || obs.manifest.PackageManager != "swiftpm" {
			return CheckResult{Verdict: "fail", Reason: "generated SwiftUI manifest metadata or config/schema hash drifted from observed project state", Remediation: "Run `attribution apply`."}
		}
		expectedApp := GeneratedAppConfig{Path: obs.project.SwiftUI.ProjectFile, Target: obs.project.SwiftUI.TargetName, InfoPlist: obs.project.SwiftUI.InfoPlistPath, GeneratedSwift: SwiftSourcePath, PackageProduct: "AttributionCore"}
		if obs.manifest.AppConfig != expectedApp {
			return CheckResult{Verdict: "fail", Reason: "generated manifest does not record the exact Xcode target integration contract", Remediation: "Run `attribution apply`."}
		}
		expected := []struct {
			path string
			raw  []byte
		}{
			{SwiftSourcePath, renderSwiftSource(*obs.config, wantedSchema)},
			{SwiftPlistPath, nil},
			{SwiftGuidePath, renderSwiftGuide(obs.project)},
		}
		expected[1].raw, _ = renderSwiftPlist(*obs.config, wantedSchema, releaseManifestForProject(obs.project.Root, *obs.config, wantedConfig, wantedSchema))
		if len(obs.manifest.GeneratedFiles) != len(expected) {
			return CheckResult{Verdict: "fail", Reason: "generated SwiftUI manifest has the wrong owned-file set", Remediation: "Run `attribution apply`."}
		}
		for index, item := range expected {
			observedRaw, found := obs.generatedRaw[item.path]
			if !found || !bytes.Equal(observedRaw, item.raw) || obs.manifest.GeneratedFiles[index].Path != item.path || obs.manifest.GeneratedFiles[index].SHA256 != sha256Hex(observedRaw) {
				return CheckResult{Verdict: "fail", Reason: "generated SwiftUI artifact or manifest hash drifted at " + item.path, Remediation: "Run `attribution apply`."}
			}
		}
		return CheckResult{Verdict: "pass", Reason: "observed config, Xcode target binding, and all generated SwiftUI artifact hashes match the deterministic manifest"}
	}
	if obs.manifest.Version != 1 || obs.manifest.GeneratedBy != "attribution "+Version || obs.manifest.Host != "expo" || obs.manifest.Mode != obs.config.Mode || obs.manifest.ConfigHash != wantedConfig || obs.manifest.SchemaHash != wantedSchema || obs.manifest.PackageManager != obs.project.PackageManager {
		return CheckResult{Verdict: "fail", Reason: "generated manifest metadata or config/schema hash drifted from observed project state", Remediation: "Run `attribution apply`."}
	}
	if obs.manifest.AppConfig != (GeneratedAppConfig{Path: "app.json", Plugin: "./" + PluginPath}) {
		return CheckResult{Verdict: "fail", Reason: "generated manifest does not record the exact app.json plugin registration", Remediation: "Run `attribution apply`."}
	}
	expected := []struct {
		path string
		raw  []byte
	}{
		{PluginPath, obs.pluginRaw},
		{ExpoFacadePath, renderExpoFacade(*obs.config)},
	}
	if len(obs.manifest.GeneratedFiles) != len(expected) {
		return CheckResult{Verdict: "fail", Reason: "generated Expo manifest has the wrong owned-file set", Remediation: "Run `attribution apply`."}
	}
	for index, item := range expected {
		observedRaw, found := obs.generatedRaw[item.path]
		if !found || !bytes.Equal(observedRaw, item.raw) || obs.manifest.GeneratedFiles[index].Path != item.path || obs.manifest.GeneratedFiles[index].SHA256 != sha256Hex(observedRaw) {
			return CheckResult{Verdict: "fail", Reason: "generated Expo artifact or manifest hash drifted at " + item.path, Remediation: "Run `attribution apply`."}
		}
	}
	return CheckResult{Verdict: "pass", Reason: "observed config, schema, and generated Expo artifact hashes match the deterministic manifest"}
}

func ruleRuntimeProbe(obs observation) CheckResult {
	if obs.probeError != "" {
		return CheckResult{
			Verdict:          "fail",
			Basis:            "unknown",
			Integrity:        "unknown",
			Comparability:    "none",
			CollectionHealth: "degraded",
			Finality:         "provisional",
			Reason:           "local runtime probe artifact is invalid: " + obs.probeError,
			Remediation:      "Run `attribution probe import --framework <expo|swiftui> --target simulator --report <path>` with a fresh exact runtime report.",
		}
	}
	if obs.probe == nil {
		return CheckResult{
			Verdict:          "unknown",
			Basis:            "unknown",
			Integrity:        "unknown",
			Comparability:    "none",
			CollectionHealth: "unknown",
			Finality:         "provisional",
			Reason:           "pending: no fresh native runtime report has been imported",
			Remediation:      "Run the configured event in an iOS simulator, save the exact AttributionUpdateReport JSON, then run `attribution probe import`.",
		}
	}
	if err := validateStoredProbe(*obs.probe, obs, time.Now().UTC()); err != nil {
		var stale *staleProbeError
		if errors.As(err, &stale) {
			return CheckResult{
				Verdict:          "unknown",
				Basis:            "measured",
				Integrity:        "copy_observed_unsigned",
				Comparability:    "exact",
				CollectionHealth: "stale",
				Finality:         "provisional",
				Reason:           "imported simulator runtime report is stale: " + err.Error(),
				Remediation:      "Run the event again and import a fresh report before verifying.",
			}
		}
		return CheckResult{
			Verdict:          "fail",
			Basis:            "unknown",
			Integrity:        "unknown",
			Comparability:    "none",
			CollectionHealth: "degraded",
			Finality:         "provisional",
			Reason:           "imported simulator runtime report no longer validates: " + err.Error(),
			Remediation:      "Run the configured event again and import a fresh exact report for the current generated plan.",
		}
	}
	return CheckResult{
		Verdict:          "pass",
		Basis:            "measured",
		Integrity:        "copy_observed_unsigned",
		Comparability:    "exact",
		CollectionHealth: "healthy",
		Finality:         "provisional",
		Reason: fmt.Sprintf(
			"fresh %s simulator report matched the current generated plan: event %s mapped exactly to conversion value %d; AdAttributionKit=%s and SKAdNetwork=%s. This is unsigned local simulator evidence only",
			obs.probe.Framework,
			obs.probe.Report.Event,
			obs.probe.Report.FineConversionValue,
			obs.probe.Report.AdAttributionKit.Status,
			obs.probe.Report.SKAdNetwork.Status,
		),
	}
}

func ruleDevicePending(observation) CheckResult {
	return CheckResult{Verdict: "unknown", Reason: "pending: no physical device-lab run has been observed; static verification cannot prove an Apple round trip"}
}

func ruleProductionPending(observation) CheckResult {
	return CheckResult{Verdict: "unknown", Reason: "pending: no production winning postback copy has been observed; local verification cannot infer production evidence"}
}

func scanSecretShapedValues(root string) ([]string, error) {
	paths, err := filesToScan(root)
	if err != nil {
		return nil, fmt.Errorf("enumerate client source for secret scan: %w", err)
	}
	var hits []string
	for _, relative := range paths {
		if relative == LastRunPath {
			continue
		}
		absolute := filepath.Join(root, filepath.FromSlash(relative))
		info, err := os.Lstat(absolute)
		if err != nil {
			return nil, fmt.Errorf("inspect %s for secret scan: %w", relative, err)
		}
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		file, err := os.Open(absolute)
		if err != nil {
			return nil, fmt.Errorf("open %s for secret scan: %w", relative, err)
		}
		sample := make([]byte, 8192)
		n, sampleErr := file.Read(sample)
		if closeErr := file.Close(); closeErr != nil {
			return nil, fmt.Errorf("close %s after secret scan sample: %w", relative, closeErr)
		}
		if sampleErr != nil && !errors.Is(sampleErr, io.EOF) {
			return nil, fmt.Errorf("sample %s for secret scan: %w", relative, sampleErr)
		}
		if bytes.IndexByte(sample[:n], 0) >= 0 {
			continue
		}
		if info.Size() > 64*1024*1024 {
			return nil, fmt.Errorf("text-like tracked file %s exceeds the 64 MiB secret-scan limit; verification cannot honestly claim full coverage", relative)
		}
		raw, err := os.ReadFile(absolute)
		if err != nil {
			return nil, fmt.Errorf("read %s for secret scan: %w", relative, err)
		}
		for _, pattern := range secretPatterns {
			if pattern.pattern.Match(raw) {
				hits = append(hits, relative+" ("+pattern.name+")")
			}
		}
	}
	sort.Strings(hits)
	return hits, nil
}

func filesToScan(root string) ([]string, error) {
	inside := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	inside.Dir = root
	if err := inside.Run(); err == nil {
		command := exec.Command("git", "ls-files", "-z", "--cached", "--others", "--exclude-standard")
		command.Dir = root
		output, err := command.Output()
		if err != nil {
			return nil, err
		}
		var paths []string
		for _, raw := range bytes.Split(output, []byte{0}) {
			if len(raw) == 0 {
				continue
			}
			path := filepath.ToSlash(string(raw))
			if !excludedScanPath(path) {
				paths = append(paths, path)
			}
		}
		sort.Strings(paths)
		return paths, nil
	}

	var paths []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if entry.IsDir() {
			if relative != "." && excludedScanDirectory(relative) {
				return filepath.SkipDir
			}
			return nil
		}
		if !excludedScanPath(relative) {
			paths = append(paths, relative)
		}
		return nil
	})
	sort.Strings(paths)
	return paths, err
}

func excludedScanDirectory(path string) bool {
	base := filepath.Base(path)
	return base == ".git" || base == "node_modules" || base == "Pods" || base == ".gradle" || base == ".expo" || base == "build" || base == "dist"
}

func excludedScanPath(path string) bool {
	parts := strings.Split(filepath.ToSlash(path), "/")
	for _, part := range parts[:maxInt(0, len(parts)-1)] {
		if excludedScanDirectory(part) {
			return true
		}
	}
	return false
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func gitRevision(root string) *string {
	command := exec.Command("git", "rev-parse", "HEAD")
	command.Dir = root
	output, err := command.Output()
	if err != nil {
		return nil
	}
	revision := strings.TrimSpace(string(output))
	if revision == "" {
		return nil
	}
	return &revision
}

func validateRunManifest(manifest RunManifest) error {
	const (
		maxRunIDLength       = 128
		maxRevisionLength    = 128
		maxBundleIDLength    = 255
		maxResults           = 128
		maxCheckIDLength     = 128
		maxRuleVersionLength = 64
		maxReasonLength      = 1000
		maxRemediationLength = 2000
	)
	stringLength := func(value string) int { return utf8.RuneCountInString(value) }
	if manifest.RunID == "" || stringLength(manifest.RunID) > maxRunIDLength || manifest.SchemaVersion != SchemaVersion || manifest.StartedAt == "" || manifest.FinishedAt == "" {
		return errors.New("missing run identity or timestamps")
	}
	if len(manifest.Results) > maxResults {
		return errors.New("run manifest has too many results")
	}
	if manifest.Environment.Revision != nil && stringLength(*manifest.Environment.Revision) > maxRevisionLength {
		return errors.New("invalid project revision")
	}
	if manifest.Project.BundleID != nil && stringLength(*manifest.Project.BundleID) > maxBundleIDLength {
		return errors.New("invalid project bundle id")
	}
	allowedDistribution := map[string]bool{"dev": true, "testflight": true, "app-store": true, "unknown": true}
	allowedProtocol := map[string]bool{"aak": true, "skan": true, "both": true, "none": true, "unknown": true}
	allowedTestMechanism := map[string]bool{"none": true, "simulator": true, "simulator-mock": true, "device-lab": true, "developer-settings": true}
	allowedPurchaseEnvironment := map[string]bool{"none": true, "storekit-test": true, "sandbox": true, "production": true, "unknown": true}
	allowedSigningKey := map[string]bool{"development": true, "distribution": true, "unknown": true}
	if !allowedDistribution[manifest.Environment.Distribution] || !allowedProtocol[manifest.Environment.Protocol] || !allowedTestMechanism[manifest.Environment.TestMechanism] || !allowedPurchaseEnvironment[manifest.Environment.PurchaseEnvironment] || !allowedSigningKey[manifest.Environment.SigningKey] {
		return errors.New("invalid run environment")
	}
	for _, digest := range []*string{manifest.Project.ConfigHash, manifest.Project.SchemaHash} {
		if digest != nil && !sha256Pattern.MatchString(*digest) {
			return errors.New("invalid project digest")
		}
	}
	allowedExecution := map[string]bool{"queued": true, "running": true, "succeeded": true, "error": true, "timed_out": true}
	allowedVerdict := map[string]bool{"pass": true, "fail": true, "unknown": true, "not_applicable": true}
	allowedSection := map[string]bool{"config": true, "build": true, "your-logic": true, "device": true, "production": true}
	allowedEvidence := map[string]bool{"static": true, "build": true, "simulator": true, "device-lab": true, "apple": true, "provider": true}
	allowedBasis := map[string]bool{"measured": true, "provider_modeled": true, "unknown": true}
	allowedIntegrity := map[string]bool{"generated": true, "observed_static": true, "apple_core_verified": true, "copy_observed_unsigned": true, "provider_claimed": true, "modeled": true, "unknown": true}
	allowedComparability := map[string]bool{"exact": true, "bounded": true, "directional": true, "none": true}
	allowedCollectionHealth := map[string]bool{"healthy": true, "degraded": true, "stale": true, "unknown": true}
	allowedFinality := map[string]bool{"provisional": true, "settled": true}
	seen := map[string]bool{}
	for _, result := range manifest.Results {
		if result.CheckID == "" || stringLength(result.CheckID) > maxCheckIDLength || seen[result.CheckID] || result.RuleVersion == "" || stringLength(result.RuleVersion) > maxRuleVersionLength || result.Reason == "" || stringLength(result.Reason) > maxReasonLength || stringLength(result.Remediation) > maxRemediationLength || !allowedSection[result.Section] || !allowedExecution[result.Execution] || !allowedVerdict[result.Verdict] || !allowedEvidence[result.Evidence] || !allowedBasis[result.Basis] || !allowedIntegrity[result.Integrity] || !allowedComparability[result.Comparability] || !allowedCollectionHealth[result.CollectionHealth] || !allowedFinality[result.Finality] {
			return fmt.Errorf("invalid result %q", result.CheckID)
		}
		seen[result.CheckID] = true
	}
	return nil
}

func HasVerificationFailures(manifest RunManifest) bool {
	for _, result := range manifest.Results {
		if result.Execution != "succeeded" || result.Verdict == "fail" {
			return true
		}
	}
	return false
}

func EncodeEvent(writer *bufio.Writer, event RunEvent) error {
	raw, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if _, err := writer.Write(append(raw, '\n')); err != nil {
		return err
	}
	return writer.Flush()
}

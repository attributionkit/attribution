package attribution

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlanAndApplyGenerateDeterministicExpoWrapper(t *testing.T) {
	root := newExpoFixture(t)
	plan, err := BuildPlan(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.ChangedPaths) != 5 {
		t.Fatalf("expected five changes, got %v", plan.ChangedPaths)
	}
	result, err := Apply(plan, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Changed) != 5 {
		t.Fatalf("unexpected apply result: %#v", result)
	}
	wrapper := string(readFixture(t, root, PluginPath))
	for _, wanted := range []string{
		`require("@attributionkit/expo/app.plugin.js")`,
		`return withAttribution(config, options)`,
		`"endpoint": "https://attribution.sh/"`,
		`"schemaHash": "` + plan.SchemaHash + `"`,
		`"metaAppId": "987654321"`,
		`"disableMetaConversionReporting": true`,
	} {
		if !strings.Contains(wrapper, wanted) {
			t.Errorf("wrapper missing %s\n%s", wanted, wrapper)
		}
	}
	if strings.Contains(wrapper, `"managed"`) {
		t.Fatalf("legacy overloaded managed option remained in wrapper:\n%s", wrapper)
	}
	project, err := DiscoverExpo(root)
	if err != nil {
		t.Fatal(err)
	}
	if !pluginRegistered(project.AppJSON) {
		t.Fatal("app.json plugin was not registered")
	}
	var manifest GeneratedManifest
	if err := decodeJSON(readFixture(t, root, ManifestPath), &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.GeneratedFiles[0].SHA256 != sha256Hex([]byte(wrapper)) {
		t.Fatal("manifest wrapper hash mismatch")
	}
}

func TestExpoPlanEmbedsOnlyPublicReleaseManifestAfterCloudBinding(t *testing.T) {
	root := newExpoFixture(t)
	applicationID := "019c0000-0000-7000-8000-000000000002"
	if err := WriteCloudBinding(root, testCloudBinding(t, "https://api.attribution.test", "org-1", applicationID, "sh.attribution.fixture")); err != nil {
		t.Fatal(err)
	}
	plan, err := BuildPlan(root)
	if err != nil {
		t.Fatal(err)
	}
	var wrapper string
	for _, operation := range plan.Operations {
		if operation.Path == PluginPath {
			wrapper = string(operation.Content)
		}
	}
	for _, wanted := range []string{
		`"appId": "` + applicationID + `"`,
		`"collectorOrigin": "https://attribution.sh/"`,
		`"associatedDomains": [`,
		`"attribution.sh"`,
		`"eventSchemaVersion": "events_4"`,
	} {
		if !strings.Contains(wrapper, wanted) {
			t.Errorf("release manifest missing %s\n%s", wanted, wrapper)
		}
	}
	for _, forbidden := range []string{"accessToken", "credential", "privateKey", "secret"} {
		if strings.Contains(wrapper, forbidden) {
			t.Fatalf("release manifest leaked forbidden field %q", forbidden)
		}
	}
}

func decodeJSON(raw []byte, target any) error {
	return json.Unmarshal(raw, target)
}

func TestSecondApplyIsNoDiffEvenFirstApplyIsUncommitted(t *testing.T) {
	root := newExpoFixture(t)
	gitFixture(t, root)
	firstPlan, err := BuildPlan(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(firstPlan, false); err != nil {
		t.Fatal(err)
	}
	secondPlan, err := BuildPlan(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(secondPlan.ChangedPaths) != 0 {
		t.Fatalf("second plan has diff: %v", secondPlan.ChangedPaths)
	}
	result, err := Apply(secondPlan, false)
	if err != nil {
		t.Fatalf("no-op apply must precede dirty guard: %v", err)
	}
	if len(result.Changed) != 0 {
		t.Fatalf("second apply changed files: %v", result.Changed)
	}
}

func TestApplyMigratesLegacyPluginRegistrationWithoutDuplicates(t *testing.T) {
	root := newExpoFixture(t)
	appJSON := map[string]any{}
	if err := json.Unmarshal(readFixture(t, root, "app.json"), &appJSON); err != nil {
		t.Fatal(err)
	}
	expo := appJSON["expo"].(map[string]any)
	expo["plugins"] = []any{
		"./" + PluginPath,
		AttributionPkg,
		[]any{AttributionEntry, map[string]any{"direct": true}},
		"./.attribution/plugin/withAttribution",
		[]any{"./.attribution/plugin/withAttribution", map[string]any{"legacy": true}},
		"expo-router",
		"react-native-fbsdk-next",
	}
	writeJSONFixture(t, root, "app.json", appJSON)
	plan, err := BuildPlan(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(plan, true); err != nil {
		t.Fatal(err)
	}
	project, err := DiscoverExpo(root)
	if err != nil {
		t.Fatal(err)
	}
	if !pluginRegistered(project.AppJSON) || legacyPluginRegistered(project.AppJSON) {
		t.Fatalf("legacy registration was not migrated: %#v", project.AppJSON)
	}
	plugins := project.AppJSON["expo"].(map[string]any)["plugins"].([]any)
	count := 0
	for _, plugin := range plugins {
		if value, ok := plugin.(string); ok && value == "./"+PluginPath {
			count++
		}
		if value, ok := plugin.([]any); ok && len(value) > 0 && value[0] == "./"+PluginPath {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected one exact registration, got %d in %#v", count, plugins)
	}
	if plugins[len(plugins)-1] != "./"+PluginPath {
		t.Fatalf("generated wrapper must be last: %#v", plugins)
	}
	for _, plugin := range plugins[:len(plugins)-1] {
		if name, ok := pluginRegistrationName(plugin); ok && isAttributionPluginRegistration(name) {
			t.Fatalf("direct or legacy Attribution registration remained: %#v", plugins)
		}
	}
}

func TestApplyRefusesUnrelatedDirtyTreeWithoutMutation(t *testing.T) {
	root := newExpoFixture(t)
	gitFixture(t, root)
	writeFixture(t, root, "src/App.tsx", "// user work\n")
	plan, err := BuildPlan(root)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Apply(plan, false)
	var dirty *DirtyWorkingTreeError
	if !errors.As(err, &dirty) {
		t.Fatalf("expected dirty-tree error, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(PluginPath))); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("plugin was mutated despite dirty-tree refusal")
	}
}

func TestUnsupportedShapeFailsBeforeMutation(t *testing.T) {
	root := newExpoFixture(t)
	before := append([]byte(nil), readFixture(t, root, "app.json")...)
	if err := os.Remove(filepath.Join(root, "app.json")); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, root, "app.config.js", "module.exports = {};\n")
	_, err := BuildPlan(root)
	var unsupported *UnsupportedProjectError
	if !errors.As(err, &unsupported) {
		t.Fatalf("expected unsupported shape, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(PluginPath))); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("plugin created before shape rejection")
	}
	_ = before
}

func TestPlanRequiresExpoPackageAndInstalledExternalAuthority(t *testing.T) {
	root := newExpoFixture(t)
	packageRaw := string(readFixture(t, root, "package.json"))
	packageRaw = strings.Replace(packageRaw, `"@attributionkit/expo": "file:../expo",`, "", 1)
	writeFixture(t, root, "package.json", packageRaw)
	_, err := BuildPlan(root)
	var missing *MissingExpoPackageError
	if !errors.As(err, &missing) {
		t.Fatalf("expected package error, got %v", err)
	}

	root = newExpoFixture(t)
	external := strings.Replace(validConfigYAML(), "mode: managed", "mode: external", 1)
	external = strings.Replace(external, "owner: managed-runtime", "owner: AppsFlyer", 1)
	writeFixture(t, root, ConfigPath, external)
	_, err = BuildPlan(root)
	var invalid *ConfigValidationError
	if !errors.As(err, &invalid) || !strings.Contains(err.Error(), "installed known conversion manager") {
		t.Fatalf("expected external authority rejection, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(PluginPath))); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("mutation occurred before external-authority rejection")
	}
}

func TestPlanRequiresLocallyResolvableExpoPluginEntrypoint(t *testing.T) {
	root := newExpoFixture(t)
	if err := os.RemoveAll(filepath.Join(root, "node_modules")); err != nil {
		t.Fatal(err)
	}
	_, err := BuildPlan(root)
	var missing *MissingExpoPackageError
	if !errors.As(err, &missing) || !strings.Contains(err.Error(), AttributionEntry) {
		t.Fatalf("expected locally resolvable package error, got %v", err)
	}
}

func TestInitUsesExistingKnownManagerAndNeverWritesLivePlaceholder(t *testing.T) {
	root := newExpoFixture(t)
	if err := os.RemoveAll(filepath.Join(root, ".attribution")); err != nil {
		t.Fatal(err)
	}
	packageRaw := string(readFixture(t, root, "package.json"))
	packageRaw = strings.Replace(packageRaw, `"expo": "^57.0.0",`, `"expo": "^57.0.0", "react-native-appsflyer": "^6.0.0", "react-native-fbsdk-next": "^13.0.0",`, 1)
	writeFixture(t, root, "package.json", packageRaw)
	created, err := Init(root)
	if err != nil {
		t.Fatal(err)
	}
	if created.Mode != "external" || created.ExternalManager != "AppsFlyer" {
		t.Fatalf("unexpected init result: %#v", created)
	}
	config := string(readFixture(t, root, ConfigPath))
	if strings.Contains(config, `appId: "0000000000"`) || !strings.Contains(config, "#   appId:") {
		t.Fatalf("starter contains a live placeholder: %s", config)
	}
}

func TestGeneratedOutputRefusesSymlink(t *testing.T) {
	root := newExpoFixture(t)
	outside := filepath.Join(t.TempDir(), "outside.js")
	writeFixture(t, filepath.Dir(outside), filepath.Base(outside), "safe\n")
	plugin := filepath.Join(root, filepath.FromSlash(PluginPath))
	if err := os.MkdirAll(filepath.Dir(plugin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, plugin); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildPlan(root); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("expected symlink refusal, got %v", err)
	}
	if !bytes.Equal(readFixture(t, filepath.Dir(outside), filepath.Base(outside)), []byte("safe\n")) {
		t.Fatal("symlink target was changed")
	}
}

func TestExactContentSymlinksNeverCountAsOwnedState(t *testing.T) {
	for _, relative := range []string{"app.json", ConfigPath, ".attribution/.gitignore", PluginPath, ManifestPath} {
		t.Run(strings.ReplaceAll(relative, "/", "_"), func(t *testing.T) {
			root := appliedFixture(t)
			raw := readFixture(t, root, relative)
			outside := filepath.Join(t.TempDir(), "exact-content")
			writeFixture(t, filepath.Dir(outside), filepath.Base(outside), string(raw))
			target := filepath.Join(root, filepath.FromSlash(relative))
			if err := os.Remove(target); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, target); err != nil {
				t.Fatal(err)
			}

			if _, err := BuildPlan(root); err == nil || !strings.Contains(err.Error(), "symlink") {
				t.Fatalf("plan accepted exact-content symlink at %s: %v", relative, err)
			}
			verified, err := RunVerify(root, nil)
			if err != nil {
				t.Fatal(err)
			}
			if !HasVerificationFailures(verified.Manifest) || verified.Manifest.Results[0].Execution != "error" || !strings.Contains(verified.Manifest.Results[0].Reason, "symlink") {
				t.Fatalf("verify accepted exact-content symlink at %s: %#v", relative, verified.Manifest.Results[0])
			}
		})
	}
}

package attribution

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func appliedFixture(t *testing.T) string {
	t.Helper()
	root := newExpoFixture(t)
	plan, err := BuildPlan(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(plan, true); err != nil {
		t.Fatal(err)
	}
	return root
}

func resultByID(t *testing.T, manifest RunManifest, id string) CheckResult {
	t.Helper()
	for _, result := range manifest.Results {
		if result.CheckID == id {
			return result
		}
	}
	t.Fatalf("result %s not found", id)
	return CheckResult{}
}

func TestVerifyAppliedExpoProjectAndHonestPendingEvidence(t *testing.T) {
	root := appliedFixture(t)
	var events []RunEvent
	verified, err := RunVerify(root, func(event RunEvent) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if HasVerificationFailures(verified.Manifest) {
		for _, result := range verified.Manifest.Results {
			if result.Verdict == "fail" || result.Execution != "succeeded" {
				t.Errorf("unexpected unhealthy result: %#v", result)
			}
		}
	}
	if verified.PersistedPath != LastRunPath {
		t.Fatalf("manifest path = %q", verified.PersistedPath)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(LastRunPath))); err != nil {
		t.Fatal(err)
	}
	if events[0].Type != "run_started" || events[len(events)-1].Type != "run_completed" {
		t.Fatalf("bad stream endpoints: %#v %#v", events[0], events[len(events)-1])
	}
	device := resultByID(t, verified.Manifest, "device.aak-postback")
	if device.Verdict != "unknown" || device.Evidence != "device-lab" || device.Integrity != "unknown" || !strings.Contains(device.Reason, "no physical") {
		t.Fatalf("device result overclaims: %#v", device)
	}
	production := resultByID(t, verified.Manifest, "production.winning-copy")
	if production.Verdict != "unknown" || production.Evidence != "apple" || production.Basis != "unknown" || !strings.Contains(production.Reason, "no production") {
		t.Fatalf("production result overclaims: %#v", production)
	}
	for _, result := range verified.Manifest.Results {
		if result.Basis == "hidden" || result.Basis == "" || result.Integrity == "" || result.Comparability == "" {
			t.Errorf("invalid evidence labels: %#v", result)
		}
	}
}

func TestVerifyChecksPackageAndExactAppJSONRegistration(t *testing.T) {
	root := appliedFixture(t)
	packageJSON := map[string]any{}
	if err := json.Unmarshal(readFixture(t, root, "package.json"), &packageJSON); err != nil {
		t.Fatal(err)
	}
	dependencies := packageJSON["dependencies"].(map[string]any)
	delete(dependencies, AttributionPkg)
	writeJSONFixture(t, root, "package.json", packageJSON)

	appJSON := map[string]any{}
	if err := json.Unmarshal(readFixture(t, root, "app.json"), &appJSON); err != nil {
		t.Fatal(err)
	}
	expo := appJSON["expo"].(map[string]any)
	expo["plugins"] = []any{"expo-router", "./.attribution/plugin/withAttribution"}
	writeJSONFixture(t, root, "app.json", appJSON)

	verified, err := RunVerify(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resultByID(t, verified.Manifest, "expo.package-installed").Verdict != "fail" {
		t.Fatal("missing package was not detected")
	}
	registration := resultByID(t, verified.Manifest, "expo.plugin-registered")
	if registration.Verdict != "fail" || !strings.Contains(registration.Reason+registration.Remediation, PluginPath) {
		t.Fatalf("missing exact .js registration was not detected: %#v", registration)
	}
}

func TestVerifyRequiresOneGeneratedWrapperInFinalPosition(t *testing.T) {
	cases := map[string][]any{
		"later plugin can overwrite":  {"./" + PluginPath, "react-native-fbsdk-next"},
		"direct package registration": {"./" + PluginPath, AttributionPkg},
		"direct entry registration":   {AttributionEntry, "./" + PluginPath},
		"duplicate generated wrapper": {"./" + PluginPath, "./" + PluginPath},
	}
	for name, plugins := range cases {
		t.Run(name, func(t *testing.T) {
			root := appliedFixture(t)
			appJSON := map[string]any{}
			if err := json.Unmarshal(readFixture(t, root, "app.json"), &appJSON); err != nil {
				t.Fatal(err)
			}
			appJSON["expo"].(map[string]any)["plugins"] = plugins
			writeJSONFixture(t, root, "app.json", appJSON)

			verified, err := RunVerify(root, nil)
			if err != nil {
				t.Fatal(err)
			}
			registration := resultByID(t, verified.Manifest, "expo.plugin-registered")
			if registration.Verdict != "fail" {
				t.Fatalf("unsafe registration passed: %#v", registration)
			}
		})
	}
}

func TestVerifyScansTrackedClientSourceForSecretShapes(t *testing.T) {
	root := appliedFixture(t)
	gitFixture(t, root)
	// Assemble the literal so the attribution repository's own source does not
	// itself contain a live-looking test credential.
	leak := `export const billing = "` + strings.Join([]string{"sk", "live", "abcdefgh12345678"}, "_") + `";\n`
	writeFixture(t, root, "src/billing.ts", leak)
	command := exec.Command("git", "add", "src/billing.ts")
	command.Dir = root
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v %s", err, output)
	}
	verified, err := RunVerify(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	secret := resultByID(t, verified.Manifest, "secrets.none-in-client")
	if secret.Verdict != "fail" || !strings.Contains(secret.Reason, "src/billing.ts") || strings.Contains(secret.Reason, "abcdefgh") {
		t.Fatalf("secret finding should name the file without echoing the value: %#v", secret)
	}
}

func TestVerifyManifestHashDrift(t *testing.T) {
	root := appliedFixture(t)
	writeFixture(t, root, PluginPath, string(readFixture(t, root, PluginPath))+"// drift\n")
	verified, err := RunVerify(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resultByID(t, verified.Manifest, "expo.plugin-wrapper").Verdict != "fail" || resultByID(t, verified.Manifest, "generated.manifest-hashes").Verdict != "fail" {
		t.Fatal("wrapper/hash drift was not detected")
	}
}

func TestVerifyMetaSDKWithoutProviderStaysAVisibleConflict(t *testing.T) {
	root := newExpoFixture(t)
	packageJSON := map[string]any{}
	if err := json.Unmarshal(readFixture(t, root, "package.json"), &packageJSON); err != nil {
		t.Fatal(err)
	}
	packageJSON["dependencies"].(map[string]any)["react-native-fbsdk-next"] = "^13.0.0"
	writeJSONFixture(t, root, "package.json", packageJSON)
	writeFixture(t, root, ConfigPath, strings.Replace(validConfigYAML(), "  meta:\n    appId: \"987654321\"\n", "", 1))
	plan, err := BuildPlan(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(plan, true); err != nil {
		t.Fatal(err)
	}
	verified, err := RunVerify(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	owner := resultByID(t, verified.Manifest, "apple.conversion-authority.single-owner")
	meta := resultByID(t, verified.Manifest, "meta.conversion-management-disabled")
	if owner.Verdict != "fail" || meta.Verdict != "fail" || !strings.Contains(meta.Reason, "providers.meta.appId") {
		t.Fatalf("undeclared Meta SDK was falsely cleared: owner=%#v meta=%#v", owner, meta)
	}
}

func TestExternalAuthorityCanKeepMetaAsDisabledEventTransport(t *testing.T) {
	root := newExpoFixture(t)
	packageJSON := map[string]any{}
	if err := json.Unmarshal(readFixture(t, root, "package.json"), &packageJSON); err != nil {
		t.Fatal(err)
	}
	dependencies := packageJSON["dependencies"].(map[string]any)
	dependencies["react-native-appsflyer"] = "^6.0.0"
	dependencies["react-native-fbsdk-next"] = "^13.0.0"
	writeJSONFixture(t, root, "package.json", packageJSON)
	writeFixture(t, root, "node_modules/react-native-appsflyer/package.json", `{"name":"react-native-appsflyer","version":"6.0.0"}`)
	external := strings.Replace(validConfigYAML(), "mode: managed", "mode: external", 1)
	external = strings.Replace(external, "owner: managed-runtime", "owner: AppsFlyer", 1)
	external = strings.Replace(external, "eventTransports: []", "eventTransports:\n  - react-native-fbsdk-next", 1)
	writeFixture(t, root, ConfigPath, external)
	plan, err := BuildPlan(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(plan, true); err != nil {
		t.Fatal(err)
	}
	wrapper := string(readFixture(t, root, PluginPath))
	if !strings.Contains(wrapper, `"disableMetaConversionReporting": true`) {
		t.Fatalf("external authority did not demote Meta transport:\n%s", wrapper)
	}
	verified, err := RunVerify(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if owner := resultByID(t, verified.Manifest, "apple.conversion-authority.single-owner"); owner.Verdict != "pass" {
		t.Fatalf("valid external authority + Meta transport failed: %#v", owner)
	}
	if meta := resultByID(t, verified.Manifest, "meta.conversion-management-disabled"); meta.Verdict != "pass" {
		t.Fatalf("Meta transport was not proven disabled: %#v", meta)
	}
}

func TestExternalMetaAuthorityIsExplicitlyEnabled(t *testing.T) {
	root := newExpoFixture(t)
	packageJSON := map[string]any{}
	if err := json.Unmarshal(readFixture(t, root, "package.json"), &packageJSON); err != nil {
		t.Fatal(err)
	}
	packageJSON["dependencies"].(map[string]any)["react-native-fbsdk-next"] = "^13.0.0"
	writeJSONFixture(t, root, "package.json", packageJSON)
	writeFixture(t, root, "node_modules/react-native-fbsdk-next/package.json", `{"name":"react-native-fbsdk-next","version":"13.0.0"}`)
	external := strings.Replace(validConfigYAML(), "mode: managed", "mode: external", 1)
	external = strings.Replace(external, "owner: managed-runtime", "owner: Meta", 1)
	writeFixture(t, root, ConfigPath, external)
	plan, err := BuildPlan(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(plan, true); err != nil {
		t.Fatal(err)
	}
	wrapper := string(readFixture(t, root, PluginPath))
	if !strings.Contains(wrapper, `"metaAppId": "987654321"`) || !strings.Contains(wrapper, `"disableMetaConversionReporting": false`) {
		t.Fatalf("external Meta authority was not explicit:\n%s", wrapper)
	}
	verified, err := RunVerify(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if meta := resultByID(t, verified.Manifest, "meta.conversion-management-disabled"); meta.Verdict != "pass" {
		t.Fatalf("external Meta authority was not proven enabled: %#v", meta)
	}
}

func TestVerifyNDJSONIsOneEventPerLine(t *testing.T) {
	root := appliedFixture(t)
	var output bytes.Buffer
	writer := bufio.NewWriter(&output)
	verified, err := RunVerify(root, func(event RunEvent) error { return EncodeEvent(writer, event) })
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 2+2*len(verified.Manifest.Results) {
		t.Fatalf("unexpected event count: %d", len(lines))
	}
	for i, line := range lines {
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("line %d is not JSON: %v", i, err)
		}
		if event["type"] == nil {
			t.Fatalf("line %d lacks type", i)
		}
	}
	var last RunEvent
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &last); err != nil {
		t.Fatal(err)
	}
	if last.Type != "run_completed" || last.Manifest == nil || last.Manifest.RunID != verified.Manifest.RunID {
		t.Fatalf("bad final event: %#v", last)
	}
}

func TestEmittedJSONFieldSetsTrackPublishedContracts(t *testing.T) {
	root := appliedFixture(t)
	var events []RunEvent
	verified, err := RunVerify(root, func(event RunEvent) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	contractRoot := filepath.Join("..", "..", "contracts")
	var manifestSchema map[string]any
	if err := json.Unmarshal(readFixture(t, contractRoot, "run-manifest.schema.json"), &manifestSchema); err != nil {
		t.Fatal(err)
	}
	manifestObject := marshalObject(t, verified.Manifest)
	assertExactKeys(t, manifestObject, objectKeys(manifestSchema["properties"]))
	assertExactKeys(t, manifestObject["environment"].(map[string]any), objectKeys(manifestSchema["properties"].(map[string]any)["environment"].(map[string]any)["properties"]))
	assertExactKeys(t, manifestObject["project"].(map[string]any), objectKeys(manifestSchema["properties"].(map[string]any)["project"].(map[string]any)["properties"]))
	resultSchema := manifestSchema["$defs"].(map[string]any)["result"].(map[string]any)
	allowedResultKeys := objectKeys(resultSchema["properties"])
	requiredResultKeys := stringSlice(resultSchema["required"])
	for _, rawResult := range manifestObject["results"].([]any) {
		result := rawResult.(map[string]any)
		assertAllowedAndRequiredKeys(t, result, allowedResultKeys, requiredResultKeys)
		if _, forbidden := result["metricBasis"]; forbidden {
			t.Fatal("metricBasis reappeared in the newer public contract")
		}
	}

	var eventSchema map[string]any
	if err := json.Unmarshal(readFixture(t, contractRoot, "run-event.schema.json"), &eventSchema); err != nil {
		t.Fatal(err)
	}
	variants := eventSchema["oneOf"].([]any)
	for _, event := range events {
		object := marshalObject(t, event)
		var matched map[string]any
		for _, rawVariant := range variants {
			variant := rawVariant.(map[string]any)
			properties := variant["properties"].(map[string]any)
			typeSchema := properties["type"].(map[string]any)
			if typeSchema["const"] == event.Type {
				matched = variant
				break
			}
		}
		if matched == nil {
			t.Fatalf("no published event variant for %s", event.Type)
		}
		assertAllowedAndRequiredKeys(t, object, objectKeys(matched["properties"]), stringSlice(matched["required"]))
	}
}

func marshalObject(t *testing.T, value any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil {
		t.Fatal(err)
	}
	return object
}

func objectKeys(value any) []string {
	object := value.(map[string]any)
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func stringSlice(value any) []string {
	values := value.([]any)
	result := make([]string, 0, len(values))
	for _, item := range values {
		result = append(result, item.(string))
	}
	sort.Strings(result)
	return result
}

func assertExactKeys(t *testing.T, object map[string]any, expected []string) {
	t.Helper()
	actual := objectKeys(object)
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("JSON keys differ from public contract: got %v want %v", actual, expected)
	}
}

func assertAllowedAndRequiredKeys(t *testing.T, object map[string]any, allowed, required []string) {
	t.Helper()
	allowedSet := make(map[string]bool, len(allowed))
	for _, key := range allowed {
		allowedSet[key] = true
	}
	for key := range object {
		if !allowedSet[key] {
			t.Fatalf("JSON field %q is not allowed by public contract", key)
		}
	}
	for _, key := range required {
		if _, found := object[key]; !found {
			t.Fatalf("JSON field %q is required by public contract", key)
		}
	}
}

package attribution

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func newExpoFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	packageJSON := map[string]any{
		"name":    "fixture",
		"private": true,
		"dependencies": map[string]any{
			"expo":         "^57.0.0",
			AttributionPkg: "file:../expo",
			"react":        "19.1.0",
			"react-native": "0.82.1",
		},
	}
	appJSON := map[string]any{
		"expo": map[string]any{
			"name":    "Fixture",
			"slug":    "fixture",
			"ios":     map[string]any{"bundleIdentifier": "sh.attribution.fixture"},
			"plugins": []any{"expo-router"},
		},
	}
	writeJSONFixture(t, root, "package.json", packageJSON)
	writeJSONFixture(t, root, "package-lock.json", map[string]any{"name": "fixture", "lockfileVersion": 3})
	writeJSONFixture(t, root, "app.json", appJSON)
	writeFixture(t, root, "node_modules/@attributionkit/expo/app.plugin.js", "module.exports = function(config) { return config; };\n")
	writeFixture(t, root, "src/App.tsx", "export default function App() { return null; }\n")
	writeFixture(t, root, ConfigPath, validConfigYAML())
	return root
}

func validConfigYAML() string {
	return `version: 1
mode: managed
app:
  bundleId: sh.attribution.fixture
conversionAuthority:
  owner: managed-runtime
eventTransports: []
providers:
  apple:
    endpoint: https://attribution.sh
    skAdNetworkIds:
      - cstr6suwn9.skadnetwork
  meta:
    appId: "987654321"
schema:
  events:
    - install
    - trial
    - purchase
    - retention
`
}

func writeFixture(t *testing.T, root, relative, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeJSONFixture(t *testing.T, root, relative string, value any) {
	t.Helper()
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, root, relative, string(raw)+"\n")
}

func readFixture(t *testing.T, root, relative string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func gitFixture(t *testing.T, root string) {
	t.Helper()
	commands := [][]string{
		{"init", "-q"},
		{"add", "-A"},
		{"-c", "user.email=test@example.com", "-c", "user.name=Test", "commit", "-qm", "fixture"},
	}
	for _, args := range commands {
		command := exec.Command("git", args...)
		command.Dir = root
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, output)
		}
	}
}

func runCLIForTest(args ...string) (int, string, string) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := RunCLI(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

package attribution

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoverExpoDerivesPackageManager(t *testing.T) {
	root := newExpoFixture(t)
	project, err := DiscoverExpo(root)
	if err != nil {
		t.Fatal(err)
	}
	if project.Host != "expo" || project.PackageManager != "npm" || project.BundleID != "sh.attribution.fixture" {
		t.Fatalf("unexpected discovery: %#v", project)
	}
}

func TestDiscoverExpoRejectsDynamicAndUnsupportedPluginShapes(t *testing.T) {
	root := newExpoFixture(t)
	if err := os.Remove(filepath.Join(root, "app.json")); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, root, "app.config.ts", "export default {};\n")
	if _, err := DiscoverExpo(root); err == nil || !strings.Contains(err.Error(), "dynamic Expo config") {
		t.Fatalf("expected dynamic config rejection, got %v", err)
	}

	root = newExpoFixture(t)
	writeFixture(t, root, "app.json", `{"expo":{"plugins":{"bad":true}}}`)
	if _, err := DiscoverExpo(root); err == nil || !strings.Contains(err.Error(), "plugins must be an array") {
		t.Fatalf("expected plugins shape rejection, got %v", err)
	}
}

func TestDiscoverExpoRejectsMixedLockfiles(t *testing.T) {
	root := newExpoFixture(t)
	writeFixture(t, root, "yarn.lock", "# yarn\n")
	if _, err := DiscoverExpo(root); err == nil || !strings.Contains(err.Error(), "multiple package-manager lockfiles") {
		t.Fatalf("expected mixed-lockfile rejection, got %v", err)
	}
}

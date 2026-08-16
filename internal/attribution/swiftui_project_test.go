package attribution

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoverSwiftUIFindsOneLiteralApplicationTarget(t *testing.T) {
	root := newSwiftUIFixture(t, swiftFixtureOptions{packageLinked: true, sourceTargeted: true, explicitPlist: true})
	project, err := DiscoverProject(root)
	if err != nil {
		t.Fatal(err)
	}
	if project.Host != "swiftui" || project.PackageManager != "swiftpm" || project.BundleID != "sh.attribution.fixture" || project.SwiftUI.TargetName != "Fixture" || project.SwiftUI.InfoPlistPath != "Fixture/Info.plist" {
		t.Fatalf("unexpected discovery: %#v / %#v", project, project.SwiftUI)
	}
	integration := inspectSwiftUIIntegration(project)
	if !integration.PackageLinked || !integration.SourceTargeted {
		t.Fatalf("valid target integration not discovered: %#v", integration)
	}
}

func TestDiscoverSwiftUIRejectsUnresolvableBuildSettings(t *testing.T) {
	for name, options := range map[string]swiftFixtureOptions{
		"bundle variable": {variableBundle: true},
		"plist variable":  {explicitPlist: true, variablePlist: true},
	} {
		t.Run(name, func(t *testing.T) {
			root := newSwiftUIFixture(t, options)
			if _, err := DiscoverProject(root); err == nil || !strings.Contains(err.Error(), "explicit") {
				t.Fatalf("expected strict build-setting rejection, got %v", err)
			}
		})
	}
}

func TestDiscoverSwiftUIRejectsMixedInfoPlistModes(t *testing.T) {
	root := newSwiftUIFixture(t, swiftFixtureOptions{explicitPlist: true})
	path := filepath.Join(root, "Fixture.xcodeproj", "project.pbxproj")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	wanted := "GENERATE_INFOPLIST_FILE = NO;\n\t\t\t\tINFOPLIST_FILE = Fixture/Info.plist;"
	index := strings.LastIndex(string(raw), wanted)
	if index < 0 {
		t.Fatal("fixture did not contain the Release Info.plist settings")
	}
	drifted := string(raw[:index]) + "GENERATE_INFOPLIST_FILE = YES;" + string(raw[index+len(wanted):])
	if err := os.WriteFile(path, []byte(drifted), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := DiscoverProject(root); err == nil || !strings.Contains(err.Error(), "mode differs") {
		t.Fatalf("mixed Info.plist modes were accepted: %v", err)
	}
}

func TestDiscoverSwiftUIRejectsAmbiguousTargetAndSymlinkProject(t *testing.T) {
	root := newSwiftUIFixture(t, swiftFixtureOptions{})
	path := filepath.Join(root, "Fixture.xcodeproj", "project.pbxproj")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	extra := `
		222222222222222222222203 /* Other */ = {
			isa = PBXNativeTarget;
			buildConfigurationList = 111111111111111111111104;
			buildPhases = (111111111111111111111107,);
			name = Other;
			packageProductDependencies = ();
			productType = "com.apple.product-type.application";
		};`
	raw = []byte(strings.Replace(string(raw), "\n\t};\n\trootObject", extra+"\n\t};\n\trootObject", 1))
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := DiscoverProject(root); err == nil || !strings.Contains(err.Error(), "exactly one iOS application target") {
		t.Fatalf("expected target ambiguity rejection, got %v", err)
	}

	root = t.TempDir()
	outside := filepath.Join(t.TempDir(), "Outside.xcodeproj")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "Fixture.xcodeproj")); err != nil {
		t.Fatal(err)
	}
	if _, err := DiscoverProject(root); err == nil || !strings.Contains(err.Error(), "non-symlink") {
		t.Fatalf("expected symlink rejection, got %v", err)
	}
}

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
    associatedDomains:
      - attribution.sh
    publisherMode: true
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

func validSwiftConfigYAML() string {
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
    associatedDomains:
      - attribution.sh
    publisherMode: true
    skAdNetworkIds:
      - cstr6suwn9.skadnetwork
schema:
  events:
    - install
    - trial
    - purchase
    - retention
`
}

type swiftFixtureOptions struct {
	packageLinked  bool
	sourceTargeted bool
	explicitPlist  bool
	variableBundle bool
	variablePlist  bool
}

func newSwiftUIFixture(t *testing.T, options swiftFixtureOptions) string {
	t.Helper()
	root := t.TempDir()
	bundle := "sh.attribution.fixture"
	if options.variableBundle {
		bundle = "$(APP_BUNDLE_ID)"
	}
	infoSettings := "GENERATE_INFOPLIST_FILE = YES;"
	if options.explicitPlist {
		infoPath := "Fixture/Info.plist"
		if options.variablePlist {
			infoPath = "$(SRCROOT)/Fixture/Info.plist"
		}
		infoSettings = "GENERATE_INFOPLIST_FILE = NO;\n\t\t\t\tINFOPLIST_FILE = " + infoPath + ";"
	}
	packageDependencies := ""
	packageObjects := ""
	frameworkBuildFile := ""
	if options.packageLinked {
		packageDependencies = "11111111111111111111110A /* AttributionCore */ ,"
		frameworkBuildFile = "11111111111111111111110D /* AttributionCore in Frameworks */ ,"
		packageObjects = `
		11111111111111111111110D /* AttributionCore in Frameworks */ = {
			isa = PBXBuildFile;
			productRef = 11111111111111111111110A /* AttributionCore */;
		};
		11111111111111111111110A /* AttributionCore */ = {
			isa = XCSwiftPackageProductDependency;
			package = 11111111111111111111110B /* XCRemoteSwiftPackageReference \"attribution\" */;
			productName = AttributionCore;
		};
		11111111111111111111110B /* XCRemoteSwiftPackageReference \"attribution\" */ = {
			isa = XCRemoteSwiftPackageReference;
			repositoryURL = "https://github.com/attributionkit/attribution";
			requirement = { kind = upToNextMajorVersion; minimumVersion = 0.1.0; };
		};`
	}
	sourceBuildFile := ""
	sourceObjects := ""
	groupChildren := ""
	if options.sourceTargeted {
		sourceBuildFile = "111111111111111111111108 /* AttributionKit.generated.swift in Sources */ ,"
		groupChildren = "111111111111111111111109 /* AttributionKit.generated.swift */ ,"
		sourceObjects = `
		111111111111111111111108 /* AttributionKit.generated.swift in Sources */ = {
			isa = PBXBuildFile;
			fileRef = 111111111111111111111109 /* AttributionKit.generated.swift */;
		};
		111111111111111111111109 /* AttributionKit.generated.swift */ = {
			isa = PBXFileReference;
			lastKnownFileType = sourcecode.swift;
			path = ".attribution/swift/AttributionKit.generated.swift";
			sourceTree = "<group>";
		};`
	}
	pbx := `// !$*UTF8*$!
{
	archiveVersion = 1;
	objectVersion = 60;
	objects = {
		111111111111111111111101 /* Project object */ = {
			isa = PBXProject;
			mainGroup = 111111111111111111111102;
			packageReferences = (11111111111111111111110B,);
			targets = (111111111111111111111103,);
		};
		111111111111111111111102 = {
			isa = PBXGroup;
			children = (` + groupChildren + `);
			sourceTree = "<group>";
		};
		111111111111111111111103 /* Fixture */ = {
			isa = PBXNativeTarget;
			buildConfigurationList = 111111111111111111111104;
			buildPhases = (111111111111111111111107, 11111111111111111111110C,);
			name = Fixture;
			packageProductDependencies = (` + packageDependencies + `);
			productType = "com.apple.product-type.application";
		};
		111111111111111111111104 = {
			isa = XCConfigurationList;
			buildConfigurations = (111111111111111111111105, 111111111111111111111106,);
		};
		111111111111111111111105 /* Debug */ = {
			isa = XCBuildConfiguration;
			buildSettings = {
				PRODUCT_BUNDLE_IDENTIFIER = ` + bundle + `;
				` + infoSettings + `
			};
			name = Debug;
		};
		111111111111111111111106 /* Release */ = {
			isa = XCBuildConfiguration;
			buildSettings = {
				PRODUCT_BUNDLE_IDENTIFIER = ` + bundle + `;
				` + infoSettings + `
			};
			name = Release;
		};
		111111111111111111111107 /* Sources */ = {
			isa = PBXSourcesBuildPhase;
			files = (` + sourceBuildFile + `);
		};
		11111111111111111111110C /* Frameworks */ = {
			isa = PBXFrameworksBuildPhase;
			files = (` + frameworkBuildFile + `);
		};` + sourceObjects + packageObjects + `
	};
	rootObject = 111111111111111111111101;
}
`
	writeFixture(t, root, "Fixture.xcodeproj/project.pbxproj", pbx)
	writeFixture(t, root, "Fixture/ContentView.swift", "import SwiftUI\nstruct ContentView: View { var body: some View { Text(\"Fixture\") } }\n")
	writeFixture(t, root, ConfigPath, validSwiftConfigYAML())
	if options.explicitPlist && !options.variablePlist {
		config, err := ParseConfig([]byte(validSwiftConfigYAML()))
		if err != nil {
			t.Fatal(err)
		}
		plist, err := renderSwiftPlist(config, schemaHash(config), nil)
		if err != nil {
			t.Fatal(err)
		}
		writeFixture(t, root, "Fixture/Info.plist", string(plist))
	}
	return root
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

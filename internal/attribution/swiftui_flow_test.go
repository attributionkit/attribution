package attribution

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func appliedSwiftFixture(t *testing.T, options swiftFixtureOptions) string {
	t.Helper()
	root := newSwiftUIFixture(t, options)
	plan, err := BuildPlan(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(plan, true); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestSwiftUICLIInitPlanApplyFlow(t *testing.T) {
	root := newSwiftUIFixture(t, swiftFixtureOptions{packageLinked: true, sourceTargeted: true, explicitPlist: true})
	if err := os.RemoveAll(filepath.Join(root, ".attribution")); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runCLIForTest("init", "--project", root)
	if code != 0 || stderr != "" || !strings.Contains(stdout, "host: swiftui") || !strings.Contains(stdout, AttributionRepo) || !strings.Contains(stdout, "AttributionCore") {
		t.Fatalf("SwiftUI init code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	code, stdout, stderr = runCLIForTest("plan", "--project", root)
	if code != 0 || stderr != "" || !strings.Contains(stdout, "SwiftUI/Fixture") || !strings.Contains(stdout, "5 file(s) would change") {
		t.Fatalf("SwiftUI plan code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	code, stdout, stderr = runCLIForTest("apply", "--project", root)
	if code != 0 || stderr != "" || !strings.Contains(stdout, "Applied 5 change(s)") {
		t.Fatalf("SwiftUI apply code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestSwiftUIPlanApplyVerifyEndToEnd(t *testing.T) {
	root := newSwiftUIFixture(t, swiftFixtureOptions{packageLinked: true, sourceTargeted: true, explicitPlist: true})
	pbxBefore := readFixture(t, root, "Fixture.xcodeproj/project.pbxproj")
	infoBefore := readFixture(t, root, "Fixture/Info.plist")
	plan, err := BuildPlan(root)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Project.Host != "swiftui" || len(plan.ChangedPaths) != 5 {
		t.Fatalf("unexpected SwiftUI plan: %#v", plan)
	}
	for _, operation := range plan.Operations {
		if operation.Path != ManifestPath && operation.Path != ".attribution/.gitignore" && !strings.HasPrefix(operation.Path, ".attribution/swift/") {
			t.Fatalf("plan attempted to mutate user-owned host file %s", operation.Path)
		}
	}
	if _, err := Apply(plan, false); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(pbxBefore, readFixture(t, root, "Fixture.xcodeproj/project.pbxproj")) || !bytes.Equal(infoBefore, readFixture(t, root, "Fixture/Info.plist")) {
		t.Fatal("SwiftUI apply mutated a human-owned Xcode project or target Info.plist")
	}
	generatedSource := string(readFixture(t, root, SwiftSourcePath))
	for _, wanted := range []string{"import AttributionCore", "AttributionConfiguration.fromBundle", `AttributionKitGeneratedPlan`, `static func record`, `"install": 0`} {
		if !strings.Contains(generatedSource, wanted) {
			t.Fatalf("generated native source missing %q:\n%s", wanted, generatedSource)
		}
	}
	if strings.Contains(generatedSource, "try!") {
		t.Fatal("generated native source force-unwraps configuration")
	}
	second, err := BuildPlan(root)
	if err != nil || len(second.ChangedPaths) != 0 {
		t.Fatalf("second plan changed: %v %#v", err, second.ChangedPaths)
	}
	verified, err := RunVerify(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if HasVerificationFailures(verified.Manifest) {
		for _, result := range verified.Manifest.Results {
			if result.Verdict == "fail" || result.Execution != "succeeded" {
				t.Errorf("unexpected SwiftUI failure: %#v", result)
			}
		}
	}
	if resultByID(t, verified.Manifest, "swiftui.package-linked").Verdict != "pass" || resultByID(t, verified.Manifest, "swiftui.generated-source-targeted").Verdict != "pass" || resultByID(t, verified.Manifest, "swiftui.info-plist-plan").Verdict != "pass" {
		t.Fatal("SwiftUI build integration did not pass")
	}
	var manifest GeneratedManifest
	if err := json.Unmarshal(readFixture(t, root, ManifestPath), &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Host != "swiftui" || manifest.AppConfig.Target != "Fixture" || manifest.AppConfig.GeneratedSwift != SwiftSourcePath || manifest.AppConfig.PackageProduct != "AttributionCore" {
		t.Fatalf("wrong native manifest: %#v", manifest)
	}
}

func TestSwiftUIGeneratedArtifactsAloneCannotFalseGreen(t *testing.T) {
	root := appliedSwiftFixture(t, swiftFixtureOptions{})
	verified, err := RunVerify(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"swiftui.package-linked", "swiftui.generated-source-targeted", "swiftui.info-plist-plan"} {
		if result := resultByID(t, verified.Manifest, id); result.Verdict != "fail" {
			t.Fatalf("generated-only project falsely passed %s: %#v", id, result)
		}
	}
	if resultByID(t, verified.Manifest, "device.aak-postback").Verdict != "unknown" || resultByID(t, verified.Manifest, "production.winning-copy").Verdict != "unknown" {
		t.Fatal("generated-only SwiftUI state leaked into Device or Production")
	}
}

func TestSwiftUIVerifySeparatesMissingPackageAndSourceMembership(t *testing.T) {
	for name, options := range map[string]swiftFixtureOptions{
		"missing package": {sourceTargeted: true, explicitPlist: true},
		"missing source":  {packageLinked: true, explicitPlist: true},
	} {
		t.Run(name, func(t *testing.T) {
			root := appliedSwiftFixture(t, options)
			verified, err := RunVerify(root, nil)
			if err != nil {
				t.Fatal(err)
			}
			packageResult := resultByID(t, verified.Manifest, "swiftui.package-linked")
			sourceResult := resultByID(t, verified.Manifest, "swiftui.generated-source-targeted")
			if name == "missing package" && (packageResult.Verdict != "fail" || sourceResult.Verdict != "pass") {
				t.Fatalf("checks were not independent: package=%#v source=%#v", packageResult, sourceResult)
			}
			if name == "missing source" && (packageResult.Verdict != "pass" || sourceResult.Verdict != "fail") {
				t.Fatalf("checks were not independent: package=%#v source=%#v", packageResult, sourceResult)
			}
		})
	}
}

func TestSwiftUIVerifyRejectsSameNamedSourceAndUnofficialPackage(t *testing.T) {
	root := newSwiftUIFixture(t, swiftFixtureOptions{packageLinked: true, sourceTargeted: true, explicitPlist: true})
	pbxPath := filepath.Join(root, "Fixture.xcodeproj", "project.pbxproj")
	pbxRaw, err := os.ReadFile(pbxPath)
	if err != nil {
		t.Fatal(err)
	}
	pbx := strings.Replace(string(pbxRaw), `path = ".attribution/swift/AttributionKit.generated.swift";`, `path = "Fixture/AttributionKit.generated.swift";`, 1)
	pbx = strings.Replace(pbx, AttributionRepo, "https://example.com/spoof/attribution", 1)
	if err := os.WriteFile(pbxPath, []byte(pbx), 0o644); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, root, "Fixture/AttributionKit.generated.swift", "// same name, wrong source\n")
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
	if resultByID(t, verified.Manifest, "swiftui.package-linked").Verdict != "fail" || resultByID(t, verified.Manifest, "swiftui.generated-source-targeted").Verdict != "fail" {
		t.Fatal("same-named source or unofficial package falsely passed")
	}
}

func TestSwiftUIVerifyRequiresPackageInFrameworksPhase(t *testing.T) {
	root := newSwiftUIFixture(t, swiftFixtureOptions{packageLinked: true, sourceTargeted: true, explicitPlist: true})
	pbxPath := filepath.Join(root, "Fixture.xcodeproj", "project.pbxproj")
	raw, err := os.ReadFile(pbxPath)
	if err != nil {
		t.Fatal(err)
	}
	drifted := strings.Replace(string(raw), "files = (11111111111111111111110D /* AttributionCore in Frameworks */ ,);", "files = ();", 1)
	if drifted == string(raw) {
		t.Fatal("fixture Frameworks phase was not rewritten")
	}
	if err := os.WriteFile(pbxPath, []byte(drifted), 0o644); err != nil {
		t.Fatal(err)
	}
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
	if result := resultByID(t, verified.Manifest, "swiftui.package-linked"); result.Verdict != "fail" {
		t.Fatalf("unlinked package dependency falsely passed: %#v", result)
	}
}

func TestSwiftUIVerifyRejectsTargetInfoPlistDrift(t *testing.T) {
	root := appliedSwiftFixture(t, swiftFixtureOptions{packageLinked: true, sourceTargeted: true, explicitPlist: true})
	infoPath := filepath.Join(root, "Fixture", "Info.plist")
	raw, err := os.ReadFile(infoPath)
	if err != nil {
		t.Fatal(err)
	}
	drifted := strings.Replace(string(raw), "https://attribution.sh/", "https://wrong.example/", 1)
	drifted = strings.Replace(drifted, "cstr6suwn9.skadnetwork", "wrong.skadnetwork", 1)
	if err := os.WriteFile(infoPath, []byte(drifted), 0o644); err != nil {
		t.Fatal(err)
	}
	verified, err := RunVerify(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resultByID(t, verified.Manifest, "apple.endpoint.report-attribution").Verdict != "fail" || resultByID(t, verified.Manifest, "apple.skan.items-present").Verdict != "fail" {
		t.Fatal("target Info.plist drift falsely passed")
	}
}

func TestSwiftUIProbeRequiresMatchingFrameworkAndIntegratedHost(t *testing.T) {
	root := appliedSwiftFixture(t, swiftFixtureOptions{packageLinked: true, sourceTargeted: true, explicitPlist: true})
	reportPath := writeExternalProbeReport(t, validRuntimeReportJSON(t, root))
	if _, err := ImportRuntimeProbe(root, ProbeImportOptions{Framework: "expo", Target: "simulator", Report: reportPath}); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("wrong framework was accepted: %v", err)
	}
	artifact, err := ImportRuntimeProbe(root, ProbeImportOptions{Framework: "swiftui", Target: "simulator", Report: reportPath})
	if err != nil {
		t.Fatal(err)
	}
	if artifact.Framework != "swiftui" {
		t.Fatalf("wrong artifact framework: %#v", artifact)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(ProbePath))); err != nil {
		t.Fatal(err)
	}
}

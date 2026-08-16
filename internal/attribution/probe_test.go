package attribution

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func validRuntimeReportJSON(t *testing.T, root string) []byte {
	t.Helper()
	config, _, err := ReadConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	report := map[string]any{
		"event":               "install",
		"fineConversionValue": 0,
		"schemaHash":          schemaHash(config),
		"adAttributionKit": map[string]any{
			"status": "failed",
			"error":  "simulator backend diagnostic that must not be persisted",
		},
		"skAdNetwork": map[string]any{
			"status": "unavailable",
		},
	}
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	return append(raw, '\n')
}

func writeExternalProbeReport(t *testing.T, raw []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "runtime-report.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCLIProbeImportFeedsOnlyYourLogic(t *testing.T) {
	root := appliedFixture(t)
	reportRaw := validRuntimeReportJSON(t, root)
	reportPath := writeExternalProbeReport(t, reportRaw)

	code, stdout, stderr := runCLIForTest(
		"probe", "import",
		"--framework", "expo",
		"--target", "simulator",
		"--report", reportPath,
		"--project", root,
	)
	if code != 0 || stderr != "" || !strings.Contains(stdout, "affect only Your Logic") {
		t.Fatalf("probe import code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	artifactRaw := readFixture(t, root, ProbePath)
	if !strings.Contains(string(artifactRaw), sha256Hex(reportRaw)) {
		t.Fatal("probe artifact did not bind the exact source bytes")
	}
	if strings.Contains(string(artifactRaw), "simulator backend diagnostic") || strings.Contains(string(artifactRaw), reportPath) {
		t.Fatal("probe artifact persisted backend error text or the local source path")
	}
	gitignore := string(readFixture(t, root, ".attribution/.gitignore"))
	if !strings.Contains(gitignore, "probe.json") {
		t.Fatal("local probe artifact is not ignored")
	}

	code, output, stderr := runCLIForTest("verify", "--json", "--project", root)
	if code != 0 || stderr != "" {
		t.Fatalf("verify code=%d stderr=%q\n%s", code, stderr, output)
	}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	var completed RunEvent
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &completed); err != nil {
		t.Fatal(err)
	}
	if completed.Manifest == nil || completed.Manifest.Environment.TestMechanism != "simulator" {
		t.Fatalf("runtime probe did not bind the run environment: %#v", completed.Manifest)
	}
	logic := resultByID(t, *completed.Manifest, "runtime.report-imported")
	if logic.Execution != "succeeded" || logic.Verdict != "pass" || logic.Evidence != "simulator" || logic.Basis != "measured" || logic.Integrity != "copy_observed_unsigned" || logic.Comparability != "exact" || logic.CollectionHealth != "healthy" || logic.Finality != "provisional" {
		t.Fatalf("unexpected Your Logic result: %#v", logic)
	}
	device := resultByID(t, *completed.Manifest, "device.aak-postback")
	production := resultByID(t, *completed.Manifest, "production.winning-copy")
	if device.Verdict != "unknown" || device.Basis != "unknown" || device.Integrity != "unknown" || production.Verdict != "unknown" || production.Basis != "unknown" || production.Integrity != "unknown" {
		t.Fatalf("simulator probe leaked into Device/Production: device=%#v production=%#v", device, production)
	}
}

func TestProbeImportAcceptsSwiftUIAsUnsignedSourceDeclaration(t *testing.T) {
	root := appliedFixture(t)
	reportPath := writeExternalProbeReport(t, validRuntimeReportJSON(t, root))
	code, _, stderr := runCLIForTest(
		"probe", "import",
		"--framework", "swiftui",
		"--target", "simulator",
		"--report", reportPath,
		"--project", root,
	)
	if code != 0 || stderr != "" {
		t.Fatalf("SwiftUI import code=%d stderr=%q", code, stderr)
	}
	var artifact RuntimeProbeArtifact
	if err := json.Unmarshal(readFixture(t, root, ProbePath), &artifact); err != nil {
		t.Fatal(err)
	}
	if artifact.Framework != "swiftui" || artifact.Target != "simulator" {
		t.Fatalf("wrong source binding: %#v", artifact)
	}
}

func TestProbeImportRejectsMalformedMismatchedAndOverclaimingReports(t *testing.T) {
	cases := map[string]func(t *testing.T, root string, raw []byte) []byte{
		"unknown key": func(t *testing.T, _ string, raw []byte) []byte {
			var report map[string]any
			if err := json.Unmarshal(raw, &report); err != nil {
				t.Fatal(err)
			}
			report["fabricated"] = true
			encoded, _ := json.Marshal(report)
			return encoded
		},
		"missing required zero value": func(t *testing.T, _ string, raw []byte) []byte {
			var report map[string]any
			if err := json.Unmarshal(raw, &report); err != nil {
				t.Fatal(err)
			}
			delete(report, "fineConversionValue")
			encoded, _ := json.Marshal(report)
			return encoded
		},
		"unknown backend status": func(t *testing.T, _ string, raw []byte) []byte {
			return []byte(strings.Replace(string(raw), `"status":"unavailable"`, `"status":"green"`, 1))
		},
		"fabricated null error": func(t *testing.T, _ string, raw []byte) []byte {
			return []byte(strings.Replace(string(raw), `"skAdNetwork":{"status":"unavailable"}`, `"skAdNetwork":{"error":null,"status":"unavailable"}`, 1))
		},
		"mismatched value": func(t *testing.T, _ string, raw []byte) []byte {
			return []byte(strings.Replace(string(raw), `"fineConversionValue":0`, `"fineConversionValue":3`, 1))
		},
		"unknown event": func(t *testing.T, _ string, raw []byte) []byte {
			return []byte(strings.Replace(string(raw), `"event":"install"`, `"event":"subscribe"`, 1))
		},
		"mismatched schema": func(t *testing.T, _ string, raw []byte) []byte {
			var report map[string]any
			if err := json.Unmarshal(raw, &report); err != nil {
				t.Fatal(err)
			}
			report["schemaHash"] = strings.Repeat("a", 64)
			encoded, _ := json.Marshal(report)
			return encoded
		},
		"simulator success overclaim": func(t *testing.T, _ string, raw []byte) []byte {
			var report map[string]any
			if err := json.Unmarshal(raw, &report); err != nil {
				t.Fatal(err)
			}
			report["skAdNetwork"] = map[string]any{"status": "succeeded"}
			encoded, _ := json.Marshal(report)
			return encoded
		},
		"duplicate key": func(_ *testing.T, root string, _ []byte) []byte {
			config, _, _ := ReadConfig(root)
			return []byte(`{"event":"install","event":"trial","fineConversionValue":0,"schemaHash":"` + schemaHash(config) + `","adAttributionKit":{"status":"unavailable"},"skAdNetwork":{"status":"unavailable"}}`)
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			root := appliedFixture(t)
			reportPath := writeExternalProbeReport(t, mutate(t, root, validRuntimeReportJSON(t, root)))
			code, _, stderr := runCLIForTest(
				"probe", "import",
				"--framework", "expo",
				"--target", "simulator",
				"--report", reportPath,
				"--project", root,
			)
			if code != 2 || !strings.Contains(stderr, "runtime probe rejected") {
				t.Fatalf("rejection code=%d stderr=%q", code, stderr)
			}
			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(ProbePath))); !os.IsNotExist(err) {
				t.Fatal("rejected input wrote a probe artifact")
			}
		})
	}
}

func TestProbeImportRequiresCurrentExactGeneratedPlan(t *testing.T) {
	root := appliedFixture(t)
	reportPath := writeExternalProbeReport(t, validRuntimeReportJSON(t, root))
	writeFixture(t, root, PluginPath, string(readFixture(t, root, PluginPath))+"// drift\n")
	code, _, stderr := runCLIForTest("probe", "import", "--framework", "expo", "--target", "simulator", "--report", reportPath, "--project", root)
	if code != 2 || !strings.Contains(stderr, "wrapper drifted") {
		t.Fatalf("plan drift code=%d stderr=%q", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(ProbePath))); !os.IsNotExist(err) {
		t.Fatal("plan drift wrote a probe artifact")
	}
}

func TestProbeImportRequiresGeneratedIgnoreBeforeWritingLocalArtifact(t *testing.T) {
	root := appliedFixture(t)
	reportPath := writeExternalProbeReport(t, validRuntimeReportJSON(t, root))
	writeFixture(t, root, ".attribution/.gitignore", "last-run.json\n")
	code, _, stderr := runCLIForTest("probe", "import", "--framework", "expo", "--target", "simulator", "--report", reportPath, "--project", root)
	if code != 2 || !strings.Contains(stderr, "generated ignore file") {
		t.Fatalf("ignore drift code=%d stderr=%q", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(ProbePath))); !os.IsNotExist(err) {
		t.Fatal("unignored probe artifact was written")
	}
}

func TestVerifyRejectsProbeAfterExactPlanBytesChange(t *testing.T) {
	root := appliedFixture(t)
	reportPath := writeExternalProbeReport(t, validRuntimeReportJSON(t, root))
	if _, err := ImportRuntimeProbe(root, ProbeImportOptions{Framework: "expo", Target: "simulator", Report: reportPath}); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, root, ManifestPath, string(readFixture(t, root, ManifestPath))+"\n")
	verified, err := RunVerify(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	logic := resultByID(t, verified.Manifest, "runtime.report-imported")
	if logic.Verdict != "fail" || logic.CollectionHealth != "degraded" || !strings.Contains(logic.Reason, "exact generated plan bytes") {
		t.Fatalf("exact plan byte drift was accepted: %#v", logic)
	}
	if verified.Manifest.Environment.TestMechanism != "none" || resultByID(t, verified.Manifest, "device.aak-postback").Verdict != "unknown" || resultByID(t, verified.Manifest, "production.winning-copy").Verdict != "unknown" {
		t.Fatal("invalidated plan leaked beyond Your Logic")
	}
}

func TestProbeImportRejectsStaleSymlinkAndNonSimulatorInput(t *testing.T) {
	root := appliedFixture(t)
	reportPath := writeExternalProbeReport(t, validRuntimeReportJSON(t, root))
	stale := time.Now().Add(-probeFreshness - time.Minute)
	if err := os.Chtimes(reportPath, stale, stale); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := runCLIForTest("probe", "import", "--framework", "expo", "--target", "simulator", "--report", reportPath, "--project", root)
	if code != 2 || !strings.Contains(stderr, "stale") {
		t.Fatalf("stale code=%d stderr=%q", code, stderr)
	}

	freshPath := writeExternalProbeReport(t, validRuntimeReportJSON(t, root))
	symlink := filepath.Join(t.TempDir(), "linked-report.json")
	if err := os.Symlink(freshPath, symlink); err != nil {
		t.Fatal(err)
	}
	code, _, stderr = runCLIForTest("probe", "import", "--framework", "expo", "--target", "simulator", "--report", symlink, "--project", root)
	if code != 2 || !strings.Contains(stderr, "non-symlink") {
		t.Fatalf("symlink code=%d stderr=%q", code, stderr)
	}

	code, _, stderr = runCLIForTest("probe", "import", "--framework", "expo", "--target", "device", "--report", freshPath, "--project", root)
	if code != 2 || !strings.Contains(stderr, "cannot be imported") {
		t.Fatalf("device code=%d stderr=%q", code, stderr)
	}
}

func TestVerifyDowngradesExpiredProbeWithoutTouchingDeviceOrProduction(t *testing.T) {
	root := appliedFixture(t)
	reportPath := writeExternalProbeReport(t, validRuntimeReportJSON(t, root))
	artifact, err := ImportRuntimeProbe(root, ProbeImportOptions{Framework: "expo", Target: "simulator", Report: reportPath})
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().UTC().Add(-probeFreshness - time.Minute)
	artifact.ImportedAt = old.Format(time.RFC3339Nano)
	artifact.ExpiresAt = old.Add(probeFreshness).Format(time.RFC3339Nano)
	artifact.Source.ModifiedAt = old.Format(time.RFC3339Nano)
	writeJSONFixture(t, root, ProbePath, artifact)

	verified, err := RunVerify(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	logic := resultByID(t, verified.Manifest, "runtime.report-imported")
	if logic.Verdict != "unknown" || logic.CollectionHealth != "stale" || logic.Integrity != "copy_observed_unsigned" || logic.Finality != "provisional" {
		t.Fatalf("expired probe was not honestly downgraded: %#v", logic)
	}
	if verified.Manifest.Environment.TestMechanism != "none" {
		t.Fatalf("expired probe marked run as simulator-backed: %#v", verified.Manifest.Environment)
	}
	if resultByID(t, verified.Manifest, "device.aak-postback").Verdict != "unknown" || resultByID(t, verified.Manifest, "production.winning-copy").Verdict != "unknown" {
		t.Fatal("expired probe affected Device or Production")
	}
}

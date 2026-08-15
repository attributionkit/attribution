package attribution

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIEndToEndInitPlanApplyVerify(t *testing.T) {
	root := newExpoFixture(t)
	if err := os.RemoveAll(filepath.Join(root, ".attribution")); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := runCLIForTest("init", "--project", root)
	if code != 0 || stderr != "" || !strings.Contains(stdout, "Created "+ConfigPath) || !strings.Contains(stdout, "not receiving postbacks") || !strings.Contains(stdout, "npm install "+ReleaseBaseURL+"/v"+Version+"/attributionkit-expo-"+Version+".tgz") || !strings.Contains(stdout, "attribution apply --branch") {
		t.Fatalf("init code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	code, stdout, stderr = runCLIForTest("plan", "--project", root)
	if code != 0 || stderr != "" || !strings.Contains(stdout, "4 file(s) would change") || !strings.Contains(stdout, "No files modified") {
		t.Fatalf("plan code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	code, stdout, stderr = runCLIForTest("apply", "--project", root)
	if code != 0 || stderr != "" || !strings.Contains(stdout, "Applied 4 change(s)") {
		t.Fatalf("apply code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	code, stdout, stderr = runCLIForTest("apply", "--project", root)
	if code != 0 || stderr != "" || !strings.Contains(stdout, "Already in desired state (no diff)") {
		t.Fatalf("second apply code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	code, stdout, stderr = runCLIForTest("verify", "--json", "--project", root)
	if code != 0 || stderr != "" {
		t.Fatalf("verify code=%d stderr=%q\n%s", code, stderr, stdout)
	}
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	var final RunEvent
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &final); err != nil {
		t.Fatal(err)
	}
	if final.Type != "run_completed" || final.Manifest == nil || HasVerificationFailures(*final.Manifest) {
		t.Fatalf("bad final event: %#v", final)
	}
}

func TestCLIDayZeroDoesNotClaimManifestPath(t *testing.T) {
	root := newExpoFixture(t)
	if err := os.RemoveAll(filepath.Join(root, ".attribution")); err != nil {
		t.Fatal(err)
	}
	code, stdout, _ := runCLIForTest("verify", "--project", root)
	if code != 1 {
		t.Fatalf("day-zero invalid state should exit 1, got %d", code)
	}
	if !strings.Contains(stdout, "Run manifest: not written") || strings.Contains(stdout, "Run manifest: "+LastRunPath) {
		t.Fatalf("false manifest path reported:\n%s", stdout)
	}
	if _, err := os.Stat(filepath.Join(root, ".attribution")); !os.IsNotExist(err) {
		t.Fatal("day-zero verify created .attribution state")
	}
}

func TestCLICollectorErrorsAreNDJSONAndExitNonzero(t *testing.T) {
	root := appliedFixture(t)
	writeFixture(t, root, "package.json", "{not-json\n")
	code, stdout, stderr := runCLIForTest("verify", "--json", "--project", root)
	if code != 1 || stderr != "" {
		t.Fatalf("collector error code=%d stderr=%q", code, stderr)
	}
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	seenError := false
	for _, line := range lines {
		var event RunEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("invalid NDJSON: %v\n%s", err, line)
		}
		if event.Result != nil && event.Result.Execution == "error" {
			seenError = true
		}
	}
	if !seenError {
		t.Fatal("collector failure did not produce execution=error results")
	}
}

func TestInitRefusesMissingRealBundleIdentifierBeforeMutation(t *testing.T) {
	root := newExpoFixture(t)
	if err := os.RemoveAll(filepath.Join(root, ".attribution")); err != nil {
		t.Fatal(err)
	}
	app := map[string]any{}
	if err := json.Unmarshal(readFixture(t, root, "app.json"), &app); err != nil {
		t.Fatal(err)
	}
	delete(app["expo"].(map[string]any), "ios")
	writeJSONFixture(t, root, "app.json", app)
	code, _, stderr := runCLIForTest("init", "--project", root)
	if code != 2 || !strings.Contains(stderr, "set it to the app's real bundle identifier") || !strings.Contains(stderr, "No files were modified") {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(root, ".attribution")); !os.IsNotExist(err) {
		t.Fatal("init invented state despite missing bundle id")
	}
}

func TestCLIMissingPackageAndExternalAuthorityAreUsageFailures(t *testing.T) {
	root := newExpoFixture(t)
	packageJSON := map[string]any{}
	if err := json.Unmarshal(readFixture(t, root, "package.json"), &packageJSON); err != nil {
		t.Fatal(err)
	}
	delete(packageJSON["dependencies"].(map[string]any), AttributionPkg)
	writeJSONFixture(t, root, "package.json", packageJSON)
	code, _, stderr := runCLIForTest("plan", "--project", root)
	if code != 2 || !strings.Contains(stderr, AttributionPkg) {
		t.Fatalf("code=%d stderr=%q", code, stderr)
	}
}

func TestCLIVersionSurfaces(t *testing.T) {
	for _, command := range []string{"version", "--version"} {
		code, stdout, stderr := runCLIForTest(command)
		if code != 0 || stdout != "attribution "+Version+"\n" || stderr != "" {
			t.Errorf("%s: code=%d stdout=%q stderr=%q", command, code, stdout, stderr)
		}
	}
	code, _, stderr := runCLIForTest("version", "--json")
	if code != 2 || !strings.Contains(stderr, "does not accept options") {
		t.Fatalf("version options: code=%d stderr=%q", code, stderr)
	}
}

func TestCLIApplyBranchHandlesUnrelatedDirtyWork(t *testing.T) {
	root := newExpoFixture(t)
	gitFixture(t, root)
	writeFixture(t, root, "src/App.tsx", "// unrelated user work\n")
	code, stdout, stderr := runCLIForTest("apply", "--branch", "attribution/test-setup", "--project", root)
	if code != 0 || stderr != "" || !strings.Contains(stdout, "Created branch attribution/test-setup") || !strings.Contains(stdout, "Applied") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	command := exec.Command("git", "branch", "--show-current")
	command.Dir = root
	output, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(output)) != "attribution/test-setup" {
		t.Fatalf("wrong branch: %q", output)
	}
}

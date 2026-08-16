package attribution

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentMCPBindsToolsToLocalProjectWithoutExposingCredential(t *testing.T) {
	root := newExpoFixture(t)
	plan, err := BuildPlan(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(plan, false); err != nil {
		t.Fatal(err)
	}
	verified, err := RunVerify(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if verified.PersistedPath == "" {
		t.Fatal("verification did not persist the run manifest")
	}
	wantedManifest := readFixture(t, root, LastRunPath)
	liveStatus := readFixture(t, filepath.Join("..", ".."), "test-vectors/live-status-connectivity.json")
	seen := map[string]bool{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer "+testAccessToken {
			t.Fatalf("authorization = %q", request.Header.Get("Authorization"))
		}
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/v1/applications/link":
			seen["link"] = true
			json.NewEncoder(writer).Encode(map[string]string{
				"applicationId": "app_example", "organizationId": "org-1", "bundleId": "sh.attribution.fixture",
			})
		case "/v1/applications/app_example/verification-runs":
			seen["upload"] = true
			raw, _ := io.ReadAll(request.Body)
			if !bytes.Equal(raw, wantedManifest) {
				t.Fatal("MCP upload changed the exact run-manifest bytes")
			}
			writer.WriteHeader(http.StatusAccepted)
			json.NewEncoder(writer).Encode(map[string]string{
				"manifestUploadId": "upload-1", "status": "accepted",
			})
		case "/v1/applications/app_example/pings":
			seen["ping"] = true
			json.NewEncoder(writer).Encode(map[string]any{
				"pingId": "ping-1", "status": "reachable", "productionEvidence": false,
			})
		case "/v1/applications/app_example/live-status":
			seen["live"] = true
			writer.Write(liveStatus)
		default:
			t.Fatalf("unexpected request %s %s", request.Method, request.URL.Path)
		}
	}))
	defer server.Close()
	if err := WriteCloudBinding(root, testCloudBinding(t, server.URL, "org-1", "app_example", "sh.attribution.fixture")); err != nil {
		t.Fatal(err)
	}

	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2026-07-28"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`,
		agentToolCallJSON(3, "attribution_link_application"),
		agentToolCallJSON(4, "attribution_upload_run"),
		agentToolCallJSON(5, "attribution_ping"),
		agentToolCallJSON(6, "attribution_live_check"),
	}, "\n") + "\n"
	store := &memoryTokenStore{token: testAccessToken}
	var output bytes.Buffer
	if err := serveAgentMCP(root, store, strings.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}

	if strings.Contains(output.String(), testAccessToken) || strings.Contains(output.String(), "CredentialRef") {
		t.Fatal("MCP output exposed credential material")
	}
	responses := decodeAgentMCPResponses(t, output.Bytes())
	if len(responses) != 6 {
		t.Fatalf("response count = %d\n%s", len(responses), output.String())
	}
	initialize := responses[0].Result.(map[string]any)
	if initialize["protocolVersion"] != "2026-07-28" {
		t.Fatalf("protocol = %#v", initialize["protocolVersion"])
	}
	listed := responses[1].Result.(map[string]any)["tools"].([]any)
	if len(listed) != 4 {
		t.Fatalf("tool count = %d", len(listed))
	}
	for _, operation := range []string{"link", "upload", "ping", "live"} {
		if !seen[operation] {
			t.Fatalf("tool did not execute %s", operation)
		}
	}
	last := responses[len(responses)-1].Result.(map[string]any)
	structured := last["structuredContent"].(map[string]any)
	if structured["productionEvidence"] != false {
		t.Fatalf("live status = %#v", structured)
	}
}

func TestAgentMCPRejectsArgumentsAndUnknownMethodsWithoutCloudCalls(t *testing.T) {
	root := newExpoFixture(t)
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":"args","method":"tools/call","params":{"name":"attribution_ping","arguments":{"applicationId":"another-app"}}}`,
		`{"jsonrpc":"2.0","id":"method","method":"secrets/read","params":{}}`,
	}, "\n") + "\n"
	var output bytes.Buffer
	if err := serveAgentMCP(root, &memoryTokenStore{token: testAccessToken}, strings.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "does not accept arguments") || !strings.Contains(output.String(), "Method not found") {
		t.Fatalf("unexpected MCP rejection output: %s", output.String())
	}
}

func TestAgentMCPDoesNotRenderLiveStatusOutsideCanonicalTaxonomy(t *testing.T) {
	root := newExpoFixture(t)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		sections := map[string]any{
			"config":     sectionFixture("pass", "static"),
			"build":      sectionFixture("pass", "build"),
			"your-logic": sectionFixture("pass", "simulator"),
			"device":     sectionFixture("pass", "device"),
			"production": sectionFixture("unknown", "none"),
		}
		sections["device"].(map[string]any)["integrity"] = "fabricated"
		json.NewEncoder(writer).Encode(map[string]any{
			"schemaVersion": liveStatusVersion, "applicationId": "app-1",
			"productionEvidence": false, "sections": sections,
		})
	}))
	defer server.Close()
	if err := WriteCloudBinding(root, testCloudBinding(t, server.URL, "org-1", "app-1", "sh.attribution.fixture")); err != nil {
		t.Fatal(err)
	}

	result := agentMCPServer{root: root, store: &memoryTokenStore{token: testAccessToken}}.callTool(context.Background(), "attribution_live_check").(map[string]any)
	if result["isError"] != true || result["structuredContent"] != nil {
		t.Fatalf("invalid taxonomy reached MCP structured output: %#v", result)
	}
}

func TestAgentSetupRegistersOnlyAbsoluteProjectAndExecutable(t *testing.T) {
	root := newExpoFixture(t)
	if err := WriteCloudBinding(root, testCloudBinding(t, "https://api.attribution.test", "org-1", "app-1", "sh.attribution.fixture")); err != nil {
		t.Fatal(err)
	}
	options := agentCLIOptions{project: root, host: "codex", name: "attribution-fixture"}
	var executableName string
	var arguments []string
	registeredName, err := setupCodexAgent(
		root,
		options,
		func() (string, error) { return "/opt/Attribution Kit/bin/attribution", nil },
		func(name string, args ...string) ([]byte, error) {
			executableName = name
			arguments = append([]string(nil), args...)
			return nil, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if registeredName != "attribution-fixture" {
		t.Fatalf("registered name = %q", registeredName)
	}
	if executableName != "codex" {
		t.Fatalf("executable = %q", executableName)
	}
	wanted := []string{
		"mcp", "add", "attribution-fixture", "--", "/opt/Attribution Kit/bin/attribution",
		"agent", "serve", "--project", root,
	}
	if fmt.Sprint(arguments) != fmt.Sprint(wanted) {
		t.Fatalf("arguments = %#v", arguments)
	}
	if strings.Contains(fmt.Sprint(arguments), testAccessToken) || strings.Contains(fmt.Sprint(arguments), "binding-v1:") {
		t.Fatal("setup arguments exposed a credential or Keychain reference")
	}
}

func TestAgentSetupDerivesAStablePerApplicationName(t *testing.T) {
	root := newExpoFixture(t)
	binding := testCloudBinding(t, "https://api.attribution.test", "org-1", "app-1", "sh.attribution.fixture")
	if err := WriteCloudBinding(root, binding); err != nil {
		t.Fatal(err)
	}
	var arguments []string
	name, err := setupCodexAgent(
		root,
		agentCLIOptions{project: root, host: "codex"},
		func() (string, error) { return "/usr/local/bin/attribution", nil },
		func(_ string, args ...string) ([]byte, error) {
			arguments = append([]string(nil), args...)
			return nil, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	wanted, err := agentMCPNameForBinding(binding)
	if err != nil {
		t.Fatal(err)
	}
	otherApplication, err := agentMCPNameForBinding(testCloudBinding(t, "https://api.attribution.test", "org-2", "app-2", "sh.attribution.fixture"))
	if err != nil {
		t.Fatal(err)
	}
	if name != wanted || name == otherApplication {
		t.Fatalf("derived name = %q", name)
	}
	if len(arguments) < 3 || arguments[2] != name {
		t.Fatalf("registration arguments = %#v", arguments)
	}
}

func TestAgentCLIGrammarIsBounded(t *testing.T) {
	options, err := parseAgentCLIOptions("setup", []string{"--project", "/tmp/app", "--host", "codex", "--name", "attribution_app"})
	if err != nil || options.project != "/tmp/app" || options.name != "attribution_app" {
		t.Fatalf("options=%#v err=%v", options, err)
	}
	for _, args := range [][]string{
		{"--host", "other"},
		{"--name", "../../unsafe"},
		{"--name", strings.Repeat("a", 65)},
	} {
		if _, err := parseAgentCLIOptions("setup", args); err == nil {
			t.Fatalf("accepted unsafe options %#v", args)
		}
	}
	if _, err := parseAgentCLIOptions("serve", []string{"--name", "other"}); err == nil {
		t.Fatal("agent serve accepted setup-only name")
	}
}

func agentToolCallJSON(id int, name string) string {
	return fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"tools/call","params":{"name":%q,"arguments":{}}}`, id, name)
}

type decodedAgentMCPResponse struct {
	Result any
}

func decodeAgentMCPResponses(t *testing.T, raw []byte) []decodedAgentMCPResponse {
	t.Helper()
	var responses []decodedAgentMCPResponse
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	for scanner.Scan() {
		var envelope struct {
			Result any `json:"result"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		responses = append(responses, decodedAgentMCPResponse{Result: envelope.Result})
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return responses
}

func TestAgentRunManifestRejectsSymlinkEvenWhenBytesMatch(t *testing.T) {
	root := newExpoFixture(t)
	run := filepath.Join(root, filepath.FromSlash(LastRunPath))
	if err := os.MkdirAll(filepath.Dir(run), 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "elsewhere.json")
	writeFixture(t, root, "elsewhere.json", "{}\n")
	if err := os.Symlink(target, run); err != nil {
		t.Fatal(err)
	}
	_, err := readAgentRunManifest(root, CloudBinding{BundleID: "sh.attribution.fixture"})
	if err == nil {
		t.Fatal("agent accepted a symlinked exact run manifest")
	}
}

func TestAgentMCPRejectsTamperedBindingBeforeKeychainLookupOrNetwork(t *testing.T) {
	root := newExpoFixture(t)
	requests := 0
	attacker := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		writer.WriteHeader(http.StatusTeapot)
	}))
	defer attacker.Close()

	binding := testCloudBinding(t, "https://api.attribution.test", "org-1", "app-1", "sh.attribution.fixture")
	binding.BaseURL = attacker.URL
	raw, err := json.Marshal(binding)
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, root, CloudConfigPath, string(raw))

	store := &memoryTokenStore{token: testAccessToken}
	result := agentMCPServer{root: root, store: store}.callTool(context.Background(), "attribution_ping").(map[string]any)
	if result["isError"] != true {
		t.Fatalf("expected tampered binding rejection, got %#v", result)
	}
	if store.gets != 0 || requests != 0 {
		t.Fatalf("tampered binding reached keychain/network: keychain gets=%d requests=%d", store.gets, requests)
	}
}

func TestAgentMCPHonorsContextCancellationInCloudCall(t *testing.T) {
	root := newExpoFixture(t)
	server := agentMCPServer{root: root, store: &memoryTokenStore{token: testAccessToken}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := server.callTool(ctx, "attribution_ping").(map[string]any)
	if result["isError"] != true {
		t.Fatalf("expected safe error, got %#v", result)
	}
}

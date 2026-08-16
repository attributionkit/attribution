package attribution

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type memoryTokenStore struct {
	reference string
	token     string
	gets      int
}

const (
	testDeviceCode  = "test-device-code-0123456789-abcdefghijklmnop"
	testAccessToken = "test-access-token-0123456789-abcdef"
)

func testCloudBinding(t *testing.T, baseURL, organizationID, applicationID, bundleID string) CloudBinding {
	t.Helper()
	reference, normalized, err := cloudCredentialReference(baseURL, organizationID, applicationID, bundleID)
	if err != nil {
		t.Fatal(err)
	}
	return CloudBinding{
		SchemaVersion:  cloudBindingVersion,
		BaseURL:        normalized,
		OrganizationID: organizationID,
		ApplicationID:  applicationID,
		BundleID:       bundleID,
		CredentialRef:  reference,
	}
}

func (s *memoryTokenStore) Set(reference, token string) error {
	s.reference = reference
	s.token = token
	return nil
}

func (s *memoryTokenStore) Get(reference string) (string, error) {
	s.gets++
	return s.token, nil
}

func TestConnectUsesPossessionProofAndStoresOnlyNonSecretBinding(t *testing.T) {
	root := newExpoFixture(t)
	store := &memoryTokenStore{}
	expires := time.Now().Add(time.Minute).UTC().Format(time.RFC3339)
	requests := 0
	exchangeRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/v1/cli/authorization-sessions":
			if request.Method != http.MethodPost {
				t.Fatalf("authorization method = %s", request.Method)
			}
			var body AuthorizationSessionRequest
			decodeTestRequest(t, request, &body)
			if body.CLIVersion != Version || body.Project.BundleID != "sh.attribution.fixture" {
				t.Fatalf("authorization request = %#v", body)
			}
			writer.WriteHeader(http.StatusCreated)
			json.NewEncoder(writer).Encode(map[string]any{
				"authorizationSessionId":  "session-1",
				"deviceCode":              testDeviceCode,
				"verificationUri":         "http://localhost:3300/cli/authorize",
				"verificationUriComplete": "http://localhost:3300/cli/authorize?user_code=ABCD-EFGH",
				"userCode":                "ABCD-EFGH",
				"expiresAt":               expires,
				"pollIntervalSeconds":     1,
			})
		case "/v1/cli/authorization-sessions/session-1/exchange":
			exchangeRequests++
			var body map[string]string
			decodeTestRequest(t, request, &body)
			if body["deviceCode"] != testDeviceCode {
				t.Fatalf("device code not returned as possession proof: %#v", body)
			}
			if exchangeRequests == 1 {
				writer.Header().Set("Retry-After", "4")
				writer.WriteHeader(http.StatusTooManyRequests)
				json.NewEncoder(writer).Encode(map[string]string{"status": "slow_down"})
				return
			}
			json.NewEncoder(writer).Encode(map[string]any{
				"status": "authorized", "accessToken": testAccessToken,
				"organizationId": "org-1", "applicationId": "app-1",
			})
		case "/v1/applications/link":
			if request.Header.Get("Authorization") != "Bearer "+testAccessToken {
				t.Fatalf("authorization = %q", request.Header.Get("Authorization"))
			}
			json.NewEncoder(writer).Encode(map[string]string{
				"applicationId": "app-1", "organizationId": "org-1", "bundleId": "sh.attribution.fixture",
			})
		default:
			t.Fatalf("unexpected path %s", request.URL.Path)
		}
	}))
	defer server.Close()

	options := cloudCLIOptions{project: root, baseURL: server.URL, noBrowser: true}
	var stdout, stderr strings.Builder
	var waits []time.Duration
	wait := func(_ context.Context, delay time.Duration) error {
		waits = append(waits, delay)
		return nil
	}
	if code := runConnectWithWait(context.Background(), root, options, store, &stdout, &stderr, wait); code != 0 {
		t.Fatalf("connect code=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	if requests != 4 {
		t.Fatalf("request count = %d", requests)
	}
	if len(waits) != 2 || waits[0] != time.Second || waits[1] != 4*time.Second {
		t.Fatalf("poll waits = %v", waits)
	}
	expectedReference, _, err := cloudCredentialReference(server.URL, "org-1", "app-1", "sh.attribution.fixture")
	if err != nil {
		t.Fatal(err)
	}
	if store.reference != expectedReference || store.token != testAccessToken {
		t.Fatalf("keychain write = reference %q token %q", store.reference, store.token)
	}
	raw := readFixture(t, root, CloudConfigPath)
	if strings.Contains(string(raw), testAccessToken) || strings.Contains(string(raw), testDeviceCode) {
		t.Fatalf("cloud binding persisted authorization secret: %s", raw)
	}
	info, err := os.Stat(filepath.Join(root, filepath.FromSlash(CloudConfigPath)))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("cloud binding mode = %o", info.Mode().Perm())
	}
}

func TestCloudClientUploadsExactManifestAndLabelsConnectivityOnly(t *testing.T) {
	manifest := []byte("{\n  \"runId\": \"run-1\"\n}\n")
	wantedDigest, wantedKey := digestHeaders(manifest)
	seenUpload := false
	seenPing := false
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.Header.Get("Authorization") != "Bearer "+testAccessToken {
			t.Fatalf("authorization = %q", request.Header.Get("Authorization"))
		}
		switch request.URL.Path {
		case "/v1/applications/app-1/verification-runs":
			seenUpload = true
			raw, _ := io.ReadAll(request.Body)
			if string(raw) != string(manifest) {
				t.Fatalf("manifest bytes changed: %q", raw)
			}
			if request.Header.Get("Content-Type") != RunManifestMediaType || request.Header.Get("Content-Digest") != wantedDigest || request.Header.Get("Idempotency-Key") != wantedKey {
				t.Fatalf("upload headers = %#v", request.Header)
			}
			writer.WriteHeader(http.StatusAccepted)
			json.NewEncoder(writer).Encode(map[string]string{"manifestUploadId": "upload-1", "status": "accepted"})
		case "/v1/applications/app-1/pings":
			seenPing = true
			raw, _ := io.ReadAll(request.Body)
			digest, _ := digestHeaders(raw)
			if request.Header.Get("Content-Digest") != digest || request.Header.Get("Idempotency-Key") == "" {
				t.Fatalf("ping integrity headers = %#v", request.Header)
			}
			if !strings.Contains(string(raw), `"kind":"cli-connectivity"`) {
				t.Fatalf("ping body = %s", raw)
			}
			json.NewEncoder(writer).Encode(map[string]any{"pingId": "ping-1", "status": "reachable", "productionEvidence": false})
		default:
			t.Fatalf("unexpected path %s", request.URL.Path)
		}
	}))
	defer server.Close()
	client, err := NewCloudClient(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	upload, err := client.UploadRun(context.Background(), testAccessToken, "app-1", manifest)
	if err != nil || upload.ManifestUploadID != "upload-1" {
		t.Fatalf("upload=%#v err=%v", upload, err)
	}
	ping, err := client.Ping(context.Background(), testAccessToken, "app-1")
	if err != nil || ping.ProductionEvidence {
		t.Fatalf("ping=%#v err=%v", ping, err)
	}
	if !seenUpload || !seenPing {
		t.Fatal("expected upload and ping requests")
	}
}

func TestCloudClientLiveStatusRequiresUnifiedSections(t *testing.T) {
	fixture := readFixture(t, filepath.Join("..", ".."), "test-vectors/live-status-connectivity.json")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/v1/applications/app_example/live-status" {
			t.Fatalf("request = %s %s", request.Method, request.URL.Path)
		}
		writer.Header().Set("Content-Type", "application/json")
		writer.Write(fixture)
	}))
	defer server.Close()
	client, _ := NewCloudClient(server.URL, server.Client())
	status, err := client.GetLiveStatus(context.Background(), testAccessToken, "app_example")
	if err != nil {
		t.Fatal(err)
	}
	if status.ProductionEvidence || status.Sections["production"].Status != "unknown" || status.Sections["config"].Facts["connectivity"].Status != "pass" || status.Sections["build"].Facts["manifest"].Status != "pass" {
		t.Fatalf("status = %#v", status)
	}
}

func TestCloudClientRejectsLiveStatusOutsideCanonicalTaxonomy(t *testing.T) {
	tests := map[string]func(map[string]any){
		"extra section": func(sections map[string]any) {
			sections["invented"] = sectionFixture("pass", "static")
		},
		"invented labels": func(sections map[string]any) {
			device := sections["device"].(map[string]any)
			device["status"] = "pass"
			device["basis"] = "trust-me"
			device["integrity"] = "fabricated"
		},
		"invalid fact timestamp": func(sections map[string]any) {
			config := sections["config"].(map[string]any)
			fact := sectionFixture("pass", "connectivity")
			fact["observedAt"] = "not-a-timestamp"
			config["facts"] = map[string]any{"connectivity": fact}
		},
		"empty optional detail": func(sections map[string]any) {
			config := sections["config"].(map[string]any)
			fact := sectionFixture("pass", "connectivity")
			fact["detail"] = ""
			config["facts"] = map[string]any{"connectivity": fact}
		},
	}

	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			sections := map[string]any{
				"config":     sectionFixture("pass", "static"),
				"build":      sectionFixture("pass", "build"),
				"your-logic": sectionFixture("pass", "simulator"),
				"device":     sectionFixture("unknown", "none"),
				"production": sectionFixture("unknown", "none"),
			}
			mutate(sections)
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.Header().Set("Content-Type", "application/json")
				json.NewEncoder(writer).Encode(map[string]any{
					"schemaVersion": liveStatusVersion, "applicationId": "app-1",
					"productionEvidence": false, "sections": sections,
				})
			}))
			defer server.Close()
			client, err := NewCloudClient(server.URL, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := client.GetLiveStatus(context.Background(), testAccessToken, "app-1"); err == nil {
				t.Fatal("accepted live status outside the canonical taxonomy")
			}
		})
	}
}

func TestCloudClientRejectsMissingOrNullLiveStatusContractFields(t *testing.T) {
	tests := map[string]func(map[string]any){
		"missing production evidence": func(envelope map[string]any) {
			delete(envelope, "productionEvidence")
		},
		"null production evidence": func(envelope map[string]any) {
			envelope["productionEvidence"] = nil
		},
		"null facts": func(envelope map[string]any) {
			sections := envelope["sections"].(map[string]any)
			sections["config"].(map[string]any)["facts"] = nil
		},
		"null observedAt": func(envelope map[string]any) {
			addFactWithOptional(envelope, "observedAt", nil)
		},
		"null detail": func(envelope map[string]any) {
			addFactWithOptional(envelope, "detail", nil)
		},
	}

	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			envelope := validLiveStatusEnvelope()
			mutate(envelope)
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.Header().Set("Content-Type", "application/json")
				json.NewEncoder(writer).Encode(envelope)
			}))
			defer server.Close()
			client, err := NewCloudClient(server.URL, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := client.GetLiveStatus(context.Background(), testAccessToken, "app-1"); err == nil {
				t.Fatal("accepted missing or null live-status contract field")
			}
		})
	}
}

func validLiveStatusEnvelope() map[string]any {
	return map[string]any{
		"schemaVersion": liveStatusVersion, "applicationId": "app-1", "productionEvidence": false,
		"sections": map[string]any{
			"config":     sectionFixture("pass", "static"),
			"build":      sectionFixture("pass", "build"),
			"your-logic": sectionFixture("pass", "simulator"),
			"device":     sectionFixture("unknown", "none"),
			"production": sectionFixture("unknown", "none"),
		},
	}
}

func addFactWithOptional(envelope map[string]any, name string, value any) {
	sections := envelope["sections"].(map[string]any)
	config := sections["config"].(map[string]any)
	fact := sectionFixture("pass", "connectivity")
	fact[name] = value
	config["facts"] = map[string]any{"connectivity": fact}
}

func TestCloudClientRejectsProductionClaimWithoutVerifiedAppleReceipt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		json.NewEncoder(writer).Encode(map[string]any{
			"schemaVersion": liveStatusVersion, "applicationId": "app-1", "productionEvidence": true,
			"sections": map[string]any{
				"config":     sectionFixture("pass", "static"),
				"build":      sectionFixture("pass", "build"),
				"your-logic": sectionFixture("pass", "simulator"),
				"device":     sectionFixture("unknown", "none"),
				"production": sectionFixture("pass", "ping"),
			},
		})
	}))
	defer server.Close()
	client, _ := NewCloudClient(server.URL, server.Client())
	if _, err := client.GetLiveStatus(context.Background(), testAccessToken, "app-1"); err == nil || !strings.Contains(err.Error(), "verified Apple receipt") {
		t.Fatalf("expected Production evidence rejection, got %v", err)
	}
}

func TestCloudClientRejectsUnsafeBaseAndTypedErrors(t *testing.T) {
	for _, raw := range []string{"http://example.com", "https://user@example.com", "https://example.com/path"} {
		if _, err := NewCloudClient(raw, nil); err == nil {
			t.Fatalf("accepted unsafe base URL %q", raw)
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(writer).Encode(map[string]string{"code": "invalid_token", "message": "Sign in again"})
	}))
	defer server.Close()
	client, _ := NewCloudClient(server.URL, server.Client())
	_, err := client.GetLiveStatus(context.Background(), "bad", "app-1")
	var apiError *CloudAPIError
	if !errors.As(err, &apiError) || apiError.Status != http.StatusUnauthorized || apiError.Code != "invalid_token" {
		t.Fatalf("error = %#v", err)
	}
}

func TestCloudClientRejectsHTTPAuthorizationOutsideExplicitLoopbackMode(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusCreated)
		json.NewEncoder(writer).Encode(map[string]any{
			"authorizationSessionId": "session-1",
			"deviceCode":             testDeviceCode,
			"verificationUri":        "http://localhost:3300/cli/authorize",
			"userCode":               "ABCD-EFGH",
			"expiresAt":              time.Now().Add(time.Minute).UTC().Format(time.RFC3339),
			"pollIntervalSeconds":    1,
		})
	}))
	defer server.Close()
	client, err := NewCloudClient(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.CreateAuthorizationSession(context.Background(), "sh.attribution.fixture"); err == nil || !strings.Contains(err.Error(), "unsafe browser authorization URL") {
		t.Fatalf("expected unsafe browser URL rejection, got %v", err)
	}
}

func TestCloudClientRejectsUnboundedAuthorizationPollingInterval(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusCreated)
		json.NewEncoder(writer).Encode(map[string]any{
			"authorizationSessionId": "session-1",
			"deviceCode":             testDeviceCode,
			"verificationUri":        "http://localhost:3300/cli/authorize",
			"userCode":               "ABCD-EFGH",
			"expiresAt":              time.Now().Add(time.Minute).UTC().Format(time.RFC3339),
			"pollIntervalSeconds":    31,
		})
	}))
	defer server.Close()
	client, err := NewCloudClient(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.CreateAuthorizationSession(context.Background(), "sh.attribution.fixture"); err == nil || !strings.Contains(err.Error(), "polling interval") {
		t.Fatalf("expected bounded polling interval rejection, got %v", err)
	}
}

func TestCloudCLIOptionGrammar(t *testing.T) {
	if options, err := parseCloudCLIOptions("live-check", []string{"--project", "/tmp/example", "--json"}); err != nil || !options.json || options.project != "/tmp/example" {
		t.Fatalf("live-check options = %#v err=%v", options, err)
	}
	if _, err := parseCloudCLIOptions("ping", []string{"--json"}); err == nil {
		t.Fatal("ping accepted --json")
	}
	if _, err := parseCloudCLIOptions("runs upload", []string{"--api-base", "https://example.com"}); err == nil {
		t.Fatal("runs upload accepted an API base override outside connect")
	}
}

func sectionFixture(status, evidence string) map[string]any {
	return map[string]any{
		"status": status, "evidence": evidence, "basis": "unknown", "integrity": "unknown",
		"comparability": "none", "collectionHealth": "unknown", "finality": "provisional",
	}
}

func decodeTestRequest(t *testing.T, request *http.Request, target any) {
	t.Helper()
	if err := json.NewDecoder(request.Body).Decode(target); err != nil {
		t.Fatal(err)
	}
}

package attribution

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	RunManifestMediaType  = "application/vnd.attributionkit.run-manifest+json"
	cloudKeychainService  = "sh.attribution.cli"
	cloudBindingVersion   = "1.0.0"
	liveStatusVersion     = "1.0.0"
	maxCloudResponseBytes = 1 << 20
)

type CloudBinding struct {
	SchemaVersion  string `json:"schemaVersion"`
	BaseURL        string `json:"baseUrl"`
	OrganizationID string `json:"organizationId"`
	ApplicationID  string `json:"applicationId"`
	BundleID       string `json:"bundleId"`
	CredentialRef  string `json:"credentialRef"`
}

type AuthorizationSessionRequest struct {
	CLIVersion string `json:"cliVersion"`
	Project    struct {
		BundleID string `json:"bundleId"`
	} `json:"project"`
}

type AuthorizationSession struct {
	AuthorizationSessionID  string `json:"authorizationSessionId"`
	DeviceCode              string `json:"deviceCode"`
	VerificationURI         string `json:"verificationUri"`
	VerificationURIComplete string `json:"verificationUriComplete,omitempty"`
	UserCode                string `json:"userCode"`
	ExpiresAt               string `json:"expiresAt"`
	PollIntervalSeconds     int    `json:"pollIntervalSeconds"`
}

type AuthorizationExchange struct {
	Status         string `json:"status"`
	AccessToken    string `json:"accessToken,omitempty"`
	OrganizationID string `json:"organizationId,omitempty"`
	ApplicationID  string `json:"applicationId,omitempty"`
}

type ApplicationLink struct {
	ApplicationID  string `json:"applicationId"`
	OrganizationID string `json:"organizationId"`
	BundleID       string `json:"bundleId"`
}

type ManifestUpload struct {
	ManifestUploadID string `json:"manifestUploadId"`
	Status           string `json:"status"`
}

type ConnectivityPing struct {
	PingID             string `json:"pingId"`
	Status             string `json:"status"`
	ProductionEvidence bool   `json:"productionEvidence"`
}

type LiveStatusFact struct {
	Status           string `json:"status"`
	Evidence         string `json:"evidence"`
	Basis            string `json:"basis"`
	Integrity        string `json:"integrity"`
	Comparability    string `json:"comparability"`
	CollectionHealth string `json:"collectionHealth"`
	Finality         string `json:"finality"`
	ObservedAt       string `json:"observedAt,omitempty"`
	Detail           string `json:"detail,omitempty"`
}

type LiveStatusSection struct {
	Status           string                    `json:"status"`
	Evidence         string                    `json:"evidence"`
	Basis            string                    `json:"basis"`
	Integrity        string                    `json:"integrity"`
	Comparability    string                    `json:"comparability"`
	CollectionHealth string                    `json:"collectionHealth"`
	Finality         string                    `json:"finality"`
	Facts            map[string]LiveStatusFact `json:"facts,omitempty"`
}

type LiveStatus struct {
	SchemaVersion      string                       `json:"schemaVersion"`
	ApplicationID      string                       `json:"applicationId"`
	ProductionEvidence bool                         `json:"productionEvidence"`
	Sections           map[string]LiveStatusSection `json:"sections"`
}

type CloudAPIError struct {
	Status  int
	Code    string
	Message string
}

func (e *CloudAPIError) Error() string {
	if e.Code == "" {
		return fmt.Sprintf("hosted API returned HTTP %d: %s", e.Status, e.Message)
	}
	return fmt.Sprintf("hosted API returned HTTP %d (%s): %s", e.Status, e.Code, e.Message)
}

type CloudClient struct {
	BaseURL    string
	HTTPClient *http.Client
}

func NewCloudClient(baseURL string, client *http.Client) (*CloudClient, error) {
	normalized, err := validateCloudBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	if client == nil {
		client = &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(request *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	return &CloudClient{BaseURL: normalized, HTTPClient: client}, nil
}

func validateCloudBaseURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return "", errors.New("hosted API base URL must be an absolute HTTPS URL")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return "", errors.New("hosted API base URL must not contain credentials, a path, query, or fragment")
	}
	if parsed.Scheme == "http" && parsed.Hostname() != "localhost" && parsed.Hostname() != "127.0.0.1" && parsed.Hostname() != "::1" {
		return "", errors.New("hosted API base URL must use HTTPS except on loopback")
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}

func (c *CloudClient) CreateAuthorizationSession(ctx context.Context, bundleID string) (AuthorizationSession, error) {
	request := AuthorizationSessionRequest{CLIVersion: Version}
	request.Project.BundleID = bundleID
	var result AuthorizationSession
	if err := c.doJSON(ctx, http.MethodPost, "/v1/cli/authorization-sessions", "", request, nil, &result, http.StatusCreated); err != nil {
		return AuthorizationSession{}, err
	}
	if result.AuthorizationSessionID == "" || len(result.DeviceCode) < 32 || result.VerificationURI == "" || result.UserCode == "" || result.ExpiresAt == "" {
		return AuthorizationSession{}, errors.New("hosted API returned an incomplete authorization session")
	}
	for _, target := range []string{result.VerificationURI, result.VerificationURIComplete} {
		if target == "" {
			continue
		}
		parsed, err := url.ParseRequestURI(target)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
			return AuthorizationSession{}, errors.New("hosted API returned an unsafe browser authorization URL")
		}
	}
	if result.PollIntervalSeconds < 1 {
		result.PollIntervalSeconds = 2
	}
	return result, nil
}

func (c *CloudClient) ExchangeAuthorization(ctx context.Context, sessionID, deviceCode string) (AuthorizationExchange, bool, error) {
	var result AuthorizationExchange
	status, err := c.doJSONStatus(ctx, http.MethodPost, "/v1/cli/authorization-sessions/"+url.PathEscape(sessionID)+"/exchange", "", map[string]string{"deviceCode": deviceCode}, nil, &result, http.StatusOK, http.StatusAccepted)
	if err != nil {
		return AuthorizationExchange{}, false, err
	}
	if status == http.StatusAccepted {
		if result.Status != "pending" {
			return AuthorizationExchange{}, false, errors.New("hosted API returned an invalid pending authorization response")
		}
		return result, false, nil
	}
	if result.Status != "authorized" || len(result.AccessToken) < 32 || result.OrganizationID == "" || result.ApplicationID == "" {
		return AuthorizationExchange{}, false, errors.New("hosted API returned an incomplete authorization exchange")
	}
	return result, true, nil
}

func (c *CloudClient) LinkApplication(ctx context.Context, token, bundleID string) (ApplicationLink, error) {
	var result ApplicationLink
	if err := c.doJSON(ctx, http.MethodPost, "/v1/applications/link", token, map[string]string{"bundleId": bundleID}, nil, &result, http.StatusOK); err != nil {
		return ApplicationLink{}, err
	}
	if result.ApplicationID == "" || result.OrganizationID == "" || result.BundleID != bundleID {
		return ApplicationLink{}, errors.New("hosted API returned an invalid application link")
	}
	return result, nil
}

func (c *CloudClient) UploadRun(ctx context.Context, token, applicationID string, manifest []byte) (ManifestUpload, error) {
	if len(manifest) == 0 {
		return ManifestUpload{}, errors.New("run manifest is empty")
	}
	digest, key := digestHeaders(manifest)
	headers := map[string]string{
		"Content-Type":    RunManifestMediaType,
		"Content-Digest":  digest,
		"Idempotency-Key": key,
	}
	var result ManifestUpload
	if err := c.doRaw(ctx, http.MethodPost, "/v1/applications/"+url.PathEscape(applicationID)+"/verification-runs", token, manifest, headers, &result, http.StatusAccepted); err != nil {
		return ManifestUpload{}, err
	}
	if result.ManifestUploadID == "" || (result.Status != "accepted" && result.Status != "duplicate") {
		return ManifestUpload{}, errors.New("hosted API returned an invalid manifest upload")
	}
	return result, nil
}

func (c *CloudClient) Ping(ctx context.Context, token, applicationID string) (ConnectivityPing, error) {
	id, err := randomHex(16)
	if err != nil {
		return ConnectivityPing{}, err
	}
	body, err := json.Marshal(map[string]string{"kind": "cli-connectivity", "cliVersion": Version})
	if err != nil {
		return ConnectivityPing{}, err
	}
	digest, _ := digestHeaders(body)
	var result ConnectivityPing
	headers := map[string]string{
		"Content-Type":    "application/json",
		"Content-Digest":  digest,
		"Idempotency-Key": id,
	}
	if err := c.doRaw(ctx, http.MethodPost, "/v1/applications/"+url.PathEscape(applicationID)+"/pings", token, body, headers, &result, http.StatusOK); err != nil {
		return ConnectivityPing{}, err
	}
	if result.PingID == "" || result.Status != "reachable" || result.ProductionEvidence {
		return ConnectivityPing{}, errors.New("hosted API returned an invalid connectivity ping")
	}
	return result, nil
}

func (c *CloudClient) GetLiveStatus(ctx context.Context, token, applicationID string) (LiveStatus, error) {
	var result LiveStatus
	if err := c.doJSON(ctx, http.MethodGet, "/v1/applications/"+url.PathEscape(applicationID)+"/live-status", token, nil, nil, &result, http.StatusOK); err != nil {
		return LiveStatus{}, err
	}
	for _, section := range []string{"config", "build", "your-logic", "device", "production"} {
		value, ok := result.Sections[section]
		if !ok {
			return LiveStatus{}, fmt.Errorf("hosted API live status omitted %s", section)
		}
		if value.Status == "" || value.Evidence == "" || value.Basis == "" || value.Integrity == "" || value.Comparability == "" || value.CollectionHealth == "" || value.Finality == "" {
			return LiveStatus{}, fmt.Errorf("hosted API live status omitted evidence labels for %s", section)
		}
		for factName, fact := range value.Facts {
			if fact.Status == "" || fact.Evidence == "" || fact.Basis == "" || fact.Integrity == "" || fact.Comparability == "" || fact.CollectionHealth == "" || fact.Finality == "" {
				return LiveStatus{}, fmt.Errorf("hosted API live status omitted evidence labels for %s.%s", section, factName)
			}
		}
	}
	if result.ApplicationID != applicationID || result.SchemaVersion != liveStatusVersion {
		return LiveStatus{}, errors.New("hosted API returned an invalid live status")
	}
	production := result.Sections["production"]
	if production.Status == "pass" || result.ProductionEvidence {
		receipt, ok := production.Facts["appleReceipt"]
		if !ok || production.Status != "pass" || !result.ProductionEvidence || receipt.Status != "pass" || receipt.Integrity != "apple_core_verified" || receipt.Basis != "measured" {
			return LiveStatus{}, errors.New("hosted API attempted to claim Production without a verified Apple receipt")
		}
	}
	return result, nil
}

func digestHeaders(body []byte) (string, string) {
	digest := sha256.Sum256(body)
	return "sha-256=:" + base64.StdEncoding.EncodeToString(digest[:]) + ":", hex.EncodeToString(digest[:])
}

func randomHex(bytesCount int) (string, error) {
	raw := make([]byte, bytesCount)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("create idempotency key: %w", err)
	}
	return hex.EncodeToString(raw), nil
}

func (c *CloudClient) doJSON(ctx context.Context, method, path, token string, body any, headers map[string]string, result any, expected int) error {
	_, err := c.doJSONStatus(ctx, method, path, token, body, headers, result, expected)
	return err
}

func (c *CloudClient) doJSONStatus(ctx context.Context, method, path, token string, body any, headers map[string]string, result any, expected ...int) (int, error) {
	var raw []byte
	var err error
	if body != nil {
		raw, err = json.Marshal(body)
		if err != nil {
			return 0, fmt.Errorf("encode hosted API request: %w", err)
		}
	}
	if headers == nil {
		headers = make(map[string]string)
	}
	if body != nil {
		headers["Content-Type"] = "application/json"
	}
	return c.doRawStatus(ctx, method, path, token, raw, headers, result, expected...)
}

func (c *CloudClient) doRaw(ctx context.Context, method, path, token string, body []byte, headers map[string]string, result any, expected int) error {
	_, err := c.doRawStatus(ctx, method, path, token, body, headers, result, expected)
	return err
}

func (c *CloudClient) doRawStatus(ctx context.Context, method, path, token string, body []byte, headers map[string]string, result any, expected ...int) (int, error) {
	request, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("create hosted API request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "attribution/"+Version)
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := c.HTTPClient.Do(request)
	if err != nil {
		return 0, fmt.Errorf("hosted API request failed: %w", err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxCloudResponseBytes+1))
	if err != nil {
		return response.StatusCode, fmt.Errorf("read hosted API response: %w", err)
	}
	if len(raw) > maxCloudResponseBytes {
		return response.StatusCode, errors.New("hosted API response exceeded 1 MiB")
	}
	ok := false
	for _, status := range expected {
		if response.StatusCode == status {
			ok = true
			break
		}
	}
	if !ok {
		var envelope struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		_ = json.Unmarshal(raw, &envelope)
		if envelope.Message == "" {
			envelope.Message = http.StatusText(response.StatusCode)
		}
		return response.StatusCode, &CloudAPIError{Status: response.StatusCode, Code: envelope.Code, Message: envelope.Message}
	}
	if result == nil || response.StatusCode == http.StatusNoContent {
		return response.StatusCode, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(result); err != nil {
		return response.StatusCode, fmt.Errorf("decode hosted API response: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return response.StatusCode, errors.New("decode hosted API response: multiple JSON values")
	}
	return response.StatusCode, nil
}

func ReadCloudBinding(root string) (CloudBinding, error) {
	if err := validateSafeTarget(root, CloudConfigPath); err != nil {
		return CloudBinding{}, err
	}
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(CloudConfigPath)))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return CloudBinding{}, errors.New("project is not connected; run `attribution connect` first")
		}
		return CloudBinding{}, fmt.Errorf("read %s: %w", CloudConfigPath, err)
	}
	var binding CloudBinding
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&binding); err != nil {
		return CloudBinding{}, fmt.Errorf("invalid %s: %w", CloudConfigPath, err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return CloudBinding{}, fmt.Errorf("invalid %s: multiple JSON values", CloudConfigPath)
	}
	if binding.SchemaVersion != cloudBindingVersion || binding.ApplicationID == "" || binding.OrganizationID == "" || binding.BundleID == "" || binding.CredentialRef == "" {
		return CloudBinding{}, fmt.Errorf("invalid %s: required binding fields are missing", CloudConfigPath)
	}
	if _, err := validateCloudBaseURL(binding.BaseURL); err != nil {
		return CloudBinding{}, fmt.Errorf("invalid %s: %w", CloudConfigPath, err)
	}
	return binding, nil
}

func WriteCloudBinding(root string, binding CloudBinding) error {
	if err := validateSafeTarget(root, CloudConfigPath); err != nil {
		return err
	}
	directory := filepath.Join(root, ".attribution")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("create .attribution: %w", err)
	}
	raw, err := json.MarshalIndent(binding, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", CloudConfigPath, err)
	}
	raw = append(raw, '\n')
	temporary, err := os.CreateTemp(directory, ".cloud-*.json")
	if err != nil {
		return fmt.Errorf("create temporary cloud binding: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(raw); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryName, filepath.Join(root, filepath.FromSlash(CloudConfigPath))); err != nil {
		return fmt.Errorf("persist %s: %w", CloudConfigPath, err)
	}
	return nil
}

type TokenStore interface {
	Set(reference, token string) error
	Get(reference string) (string, error)
}

type OSKeychainTokenStore struct{}

func (OSKeychainTokenStore) Set(reference, token string) error {
	if runtime.GOOS != "darwin" {
		return errors.New("OS keychain storage is currently supported on macOS; no token was written")
	}
	if reference == "" || token == "" {
		return errors.New("keychain reference and token are required")
	}
	command := exec.Command("security", "add-generic-password", "-U", "-a", reference, "-s", cloudKeychainService, "-w")
	command.Stdin = strings.NewReader(token + "\n")
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("store access token in macOS Keychain: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (OSKeychainTokenStore) Get(reference string) (string, error) {
	if runtime.GOOS != "darwin" {
		return "", errors.New("OS keychain lookup is currently supported on macOS")
	}
	command := exec.Command("security", "find-generic-password", "-a", reference, "-s", cloudKeychainService, "-w")
	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("read access token from macOS Keychain: %w", err)
	}
	token := strings.TrimSpace(string(output))
	if token == "" {
		return "", errors.New("macOS Keychain returned an empty access token")
	}
	return token, nil
}

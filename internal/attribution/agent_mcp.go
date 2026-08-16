package attribution

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const (
	agentMCPServerName     = "attribution-local"
	agentMCPMaxMessageSize = 1 << 20
)

type agentMCPRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type agentMCPResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *agentMCPError  `json:"error,omitempty"`
}

type agentMCPError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type agentMCPTool struct {
	Name         string         `json:"name"`
	Title        string         `json:"title"`
	Description  string         `json:"description"`
	InputSchema  map[string]any `json:"inputSchema"`
	OutputSchema map[string]any `json:"outputSchema,omitempty"`
	Annotations  map[string]any `json:"annotations"`
}

type agentMCPToolCall struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type agentMCPServer struct {
	root  string
	store TokenStore
}

func serveAgentMCP(root string, store TokenStore, input io.Reader, output io.Writer) error {
	server := agentMCPServer{root: root, store: store}
	scanner := bufio.NewScanner(input)
	buffer := make([]byte, 64*1024)
	scanner.Buffer(buffer, agentMCPMaxMessageSize)
	encoder := json.NewEncoder(output)
	encoder.SetEscapeHTML(false)

	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		response, respond := server.handle(context.Background(), line)
		if !respond {
			continue
		}
		if err := encoder.Encode(response); err != nil {
			return fmt.Errorf("write MCP response: %w", err)
		}
	}
	if err := scanner.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			return errors.New("MCP request exceeded 1 MiB")
		}
		return fmt.Errorf("read MCP request: %w", err)
	}
	return nil
}

func (server agentMCPServer) handle(ctx context.Context, raw []byte) (agentMCPResponse, bool) {
	var request agentMCPRequest
	if err := json.Unmarshal(raw, &request); err != nil {
		return agentMCPFailure(json.RawMessage("null"), -32700, "Parse error"), true
	}
	if request.JSONRPC != "2.0" || request.Method == "" {
		return agentMCPFailure(request.ID, -32600, "Invalid Request"), request.ID != nil
	}
	if request.ID == nil {
		return agentMCPResponse{}, false
	}

	switch request.Method {
	case "initialize":
		var params struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(request.Params, &params)
		protocolVersion := params.ProtocolVersion
		if protocolVersion == "" || len(protocolVersion) > 64 {
			protocolVersion = "2025-06-18"
		}
		return agentMCPSuccess(request.ID, map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities": map[string]any{
				"tools": map[string]any{"listChanged": false},
			},
			"serverInfo": map[string]string{
				"name": agentMCPServerName, "version": Version,
			},
			"instructions": "These tools are bound to one locally connected repository. Credentials stay in macOS Keychain. Connectivity and simulator results never become Apple Device or Production evidence.",
		}), true
	case "ping":
		return agentMCPSuccess(request.ID, map[string]any{}), true
	case "tools/list":
		return agentMCPSuccess(request.ID, map[string]any{"tools": agentMCPTools()}), true
	case "tools/call":
		var call agentMCPToolCall
		if err := json.Unmarshal(request.Params, &call); err != nil || call.Name == "" {
			return agentMCPFailure(request.ID, -32602, "Invalid tool call"), true
		}
		if !emptyAgentMCPArguments(call.Arguments) {
			return agentMCPSuccess(request.ID, agentMCPToolFailure("invalid_arguments", "This project-bound tool does not accept arguments.")), true
		}
		result := server.callTool(ctx, call.Name)
		return agentMCPSuccess(request.ID, result), true
	default:
		return agentMCPFailure(request.ID, -32601, "Method not found"), true
	}
}

func emptyAgentMCPArguments(raw json.RawMessage) bool {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return true
	}
	var arguments map[string]json.RawMessage
	return json.Unmarshal(raw, &arguments) == nil && len(arguments) == 0
}

func (server agentMCPServer) callTool(ctx context.Context, name string) any {
	known := name == "attribution_link_application" ||
		name == "attribution_upload_run" ||
		name == "attribution_ping" ||
		name == "attribution_live_check"
	if !known {
		return agentMCPToolFailure("unknown_tool", "Unknown Attribution tool.")
	}
	client, binding, token, err := loadAgentCloud(server.root, server.store)
	if err != nil {
		return agentMCPToolFailure("project_not_ready", safeAgentMCPError(err))
	}

	switch name {
	case "attribution_link_application":
		link, err := client.LinkApplication(ctx, token, binding.BundleID)
		if err != nil {
			return agentMCPToolFailure("link_failed", safeAgentMCPError(err))
		}
		if link.ApplicationID != binding.ApplicationID || link.OrganizationID != binding.OrganizationID {
			return agentMCPToolFailure("link_mismatch", "The hosted application identity no longer matches this repository binding.")
		}
		return agentMCPToolSuccess(link, fmt.Sprintf("Confirmed linked application %s for %s.", link.ApplicationID, link.BundleID))
	case "attribution_upload_run":
		manifest, err := readAgentRunManifest(server.root, binding)
		if err != nil {
			return agentMCPToolFailure("manifest_invalid", safeAgentMCPError(err))
		}
		upload, err := client.UploadRun(ctx, token, binding.ApplicationID, manifest)
		if err != nil {
			return agentMCPToolFailure("upload_failed", safeAgentMCPError(err))
		}
		return agentMCPToolSuccess(upload, fmt.Sprintf("Uploaded the exact local run manifest (%s; %s).", upload.ManifestUploadID, upload.Status))
	case "attribution_ping":
		ping, err := client.Ping(ctx, token, binding.ApplicationID)
		if err != nil {
			return agentMCPToolFailure("ping_failed", safeAgentMCPError(err))
		}
		return agentMCPToolSuccess(ping, fmt.Sprintf("Control plane is reachable (ping %s). This is not Device, Apple, or Production evidence.", ping.PingID))
	case "attribution_live_check":
		status, err := client.GetLiveStatus(ctx, token, binding.ApplicationID)
		if err != nil {
			return agentMCPToolFailure("live_check_failed", safeAgentMCPError(err))
		}
		production := "not verified"
		if status.ProductionEvidence {
			production = "verified by qualifying Apple evidence"
		}
		return agentMCPToolSuccess(status, fmt.Sprintf("Live check read for %s; Production is %s.", status.ApplicationID, production))
	}
	return agentMCPToolFailure("unknown_tool", "Unknown Attribution tool.")
}

func loadAgentCloud(root string, store TokenStore) (*CloudClient, CloudBinding, string, error) {
	binding, err := ReadCloudBinding(root)
	if err != nil {
		return nil, CloudBinding{}, "", err
	}
	config, _, err := ReadConfig(root)
	if err != nil {
		return nil, CloudBinding{}, "", err
	}
	if config.App.BundleID != binding.BundleID {
		return nil, CloudBinding{}, "", fmt.Errorf("%s is linked to %s but %s configures %s", CloudConfigPath, binding.BundleID, ConfigPath, config.App.BundleID)
	}
	token, err := store.Get(binding.CredentialRef)
	if err != nil {
		return nil, CloudBinding{}, "", err
	}
	client, err := NewCloudClient(binding.BaseURL, nil)
	if err != nil {
		return nil, CloudBinding{}, "", err
	}
	return client, binding, token, nil
}

func readAgentRunManifest(root string, binding CloudBinding) ([]byte, error) {
	if err := validateSafeTarget(root, LastRunPath); err != nil {
		return nil, err
	}
	path := filepath.Join(root, filepath.FromSlash(LastRunPath))
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%s not found; run `attribution verify --json` first", LastRunPath)
		}
		return nil, fmt.Errorf("read %s: %w", LastRunPath, err)
	}
	if info.Size() <= 0 || info.Size() > agentMCPMaxMessageSize {
		return nil, fmt.Errorf("%s must be between 1 byte and 1 MiB", LastRunPath)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", LastRunPath, err)
	}
	var manifest RunManifest
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("invalid %s: %w", LastRunPath, err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, fmt.Errorf("invalid %s: multiple JSON values", LastRunPath)
	}
	if err := validateRunManifest(manifest); err != nil {
		return nil, fmt.Errorf("invalid %s: %w", LastRunPath, err)
	}
	if manifest.Project.BundleID == nil || *manifest.Project.BundleID != binding.BundleID {
		return nil, fmt.Errorf("%s does not belong to linked bundle %s", LastRunPath, binding.BundleID)
	}
	return raw, nil
}

func agentMCPTools() []agentMCPTool {
	emptyInput := func() map[string]any {
		return map[string]any{"type": "object", "additionalProperties": false}
	}
	return []agentMCPTool{
		{
			Name: "attribution_link_application", Title: "Confirm this repository's linked application",
			Description: "Confirms that the application-scoped Keychain credential still matches this repository. No credential enters tool data.",
			InputSchema: emptyInput(),
			Annotations: map[string]any{"readOnlyHint": true, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false},
		},
		{
			Name: "attribution_upload_run", Title: "Upload this repository's exact verification run",
			Description: "Reads .attribution/last-run.json locally and uploads its exact bytes. Device and Production claims are still independently constrained by the control plane.",
			InputSchema: emptyInput(),
			Annotations: map[string]any{"readOnlyHint": false, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false},
		},
		{
			Name: "attribution_ping", Title: "Ping the Attribution control plane",
			Description: "Records authenticated reachability for the bound app. It can never create Device, Apple, or Production evidence.",
			InputSchema: emptyInput(),
			Annotations: map[string]any{"readOnlyHint": false, "destructiveHint": false, "idempotentHint": false, "openWorldHint": false},
		},
		{
			Name: "attribution_live_check", Title: "Read this repository's live checklist",
			Description: "Reads labeled Config, Build, Your Logic, Device, and Production status. Production passes only from qualifying verified Apple evidence.",
			InputSchema: emptyInput(),
			Annotations: map[string]any{"readOnlyHint": true, "destructiveHint": false, "idempotentHint": true, "openWorldHint": false},
		},
	}
}

func agentMCPToolSuccess(value any, text string) map[string]any {
	return map[string]any{
		"content":           []map[string]string{{"type": "text", "text": text}},
		"structuredContent": value,
	}
}

func agentMCPToolFailure(code, message string) map[string]any {
	return map[string]any{
		"isError": true,
		"content": []map[string]string{{
			"type": "text", "text": code + ": " + message,
		}},
	}
}

func safeAgentMCPError(err error) string {
	message := err.Error()
	if len(message) > 512 {
		message = message[:512]
	}
	return message
}

func agentMCPSuccess(id json.RawMessage, result any) agentMCPResponse {
	return agentMCPResponse{JSONRPC: "2.0", ID: id, Result: result}
}

func agentMCPFailure(id json.RawMessage, code int, message string) agentMCPResponse {
	return agentMCPResponse{
		JSONRPC: "2.0", ID: id,
		Error: &agentMCPError{Code: code, Message: message},
	}
}

package attribution

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const defaultCloudBaseURL = "https://api.attribution.sh"

type cloudCLIOptions struct {
	project   string
	baseURL   string
	json      bool
	noBrowser bool
}

func runCloudCLI(args []string, stdout, stderr io.Writer) int {
	command := args[0]
	rest := args[1:]
	if command == "runs" {
		if len(rest) == 0 || rest[0] != "upload" {
			fmt.Fprintln(stderr, "usage: attribution runs upload [--project <dir>]")
			return 2
		}
		command = "runs upload"
		rest = rest[1:]
	}
	options, err := parseCloudCLIOptions(command, rest)
	if err != nil {
		fmt.Fprintln(stderr, err)
		printCloudUsage(stderr, command)
		return 2
	}
	root, err := filepath.Abs(options.project)
	if err != nil {
		fmt.Fprintln(stderr, "resolve project:", err)
		return 2
	}
	ctx := context.Background()
	store := OSKeychainTokenStore{}

	switch command {
	case "connect":
		return runConnect(ctx, root, options, store, stdout, stderr)
	case "runs upload", "ping", "live-check":
		binding, err := ReadCloudBinding(root)
		if err != nil {
			return renderCLIError(err, stderr)
		}
		config, _, err := ReadConfig(root)
		if err != nil {
			return renderCLIError(err, stderr)
		}
		if config.App.BundleID != binding.BundleID {
			return renderCLIError(fmt.Errorf("%s is linked to %s but %s configures %s; run `attribution connect` for the configured app", CloudConfigPath, binding.BundleID, ConfigPath, config.App.BundleID), stderr)
		}
		token, err := store.Get(binding.CredentialRef)
		if err != nil {
			return renderCLIError(err, stderr)
		}
		client, err := NewCloudClient(binding.BaseURL, nil)
		if err != nil {
			return renderCLIError(err, stderr)
		}
		switch command {
		case "runs upload":
			raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(LastRunPath)))
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					err = fmt.Errorf("%s not found; run `attribution verify --json` first", LastRunPath)
				} else {
					err = fmt.Errorf("read %s: %w", LastRunPath, err)
				}
				return renderCLIError(err, stderr)
			}
			var manifest RunManifest
			decoder := json.NewDecoder(bytes.NewReader(raw))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&manifest); err != nil {
				return renderCLIError(fmt.Errorf("invalid %s: %w", LastRunPath, err), stderr)
			}
			if err := decoder.Decode(&struct{}{}); err != io.EOF {
				return renderCLIError(fmt.Errorf("invalid %s: multiple JSON values", LastRunPath), stderr)
			}
			if err := validateRunManifest(manifest); err != nil {
				return renderCLIError(fmt.Errorf("invalid %s: %w", LastRunPath, err), stderr)
			}
			if manifest.Project.BundleID == nil || *manifest.Project.BundleID != binding.BundleID {
				return renderCLIError(fmt.Errorf("%s does not belong to linked bundle %s; run `attribution verify --json` again", LastRunPath, binding.BundleID), stderr)
			}
			upload, err := client.UploadRun(ctx, token, binding.ApplicationID, raw)
			if err != nil {
				return renderCLIError(err, stderr)
			}
			fmt.Fprintf(stdout, "Uploaded exact %s bytes (manifest upload %s; status: %s).\n", LastRunPath, upload.ManifestUploadID, upload.Status)
			return 0
		case "ping":
			ping, err := client.Ping(ctx, token, binding.ApplicationID)
			if err != nil {
				return renderCLIError(err, stderr)
			}
			fmt.Fprintf(stdout, "Hosted control plane is reachable (ping %s; status: %s).\n", ping.PingID, ping.Status)
			fmt.Fprintln(stdout, "This connectivity ping is not Apple, device, or Production evidence.")
			return 0
		case "live-check":
			status, err := client.GetLiveStatus(ctx, token, binding.ApplicationID)
			if err != nil {
				return renderCLIError(err, stderr)
			}
			if options.json {
				encoder := json.NewEncoder(stdout)
				encoder.SetEscapeHTML(false)
				if err := encoder.Encode(status); err != nil {
					return renderCLIError(err, stderr)
				}
			} else {
				renderLiveStatus(stdout, status)
			}
			return 0
		}
	}
	return 70
}

func parseCloudCLIOptions(command string, args []string) (cloudCLIOptions, error) {
	options := cloudCLIOptions{project: ".", baseURL: defaultCloudBaseURL}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--project":
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "--") {
				return cloudCLIOptions{}, errors.New("--project requires a directory")
			}
			i++
			options.project = args[i]
		case "--api-base":
			if command != "connect" {
				return cloudCLIOptions{}, errors.New("--api-base is supported only by connect")
			}
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "--") {
				return cloudCLIOptions{}, errors.New("--api-base requires a URL")
			}
			i++
			options.baseURL = args[i]
		case "--json":
			if command != "live-check" {
				return cloudCLIOptions{}, errors.New("--json is supported only by live-check")
			}
			options.json = true
		case "--no-browser":
			if command != "connect" {
				return cloudCLIOptions{}, errors.New("--no-browser is supported only by connect")
			}
			options.noBrowser = true
		default:
			return cloudCLIOptions{}, fmt.Errorf("unknown option %q", args[i])
		}
	}
	return options, nil
}

func runConnect(ctx context.Context, root string, options cloudCLIOptions, store TokenStore, stdout, stderr io.Writer) int {
	return runConnectWithWait(ctx, root, options, store, stdout, stderr, waitForCloudPoll)
}

func waitForCloudPoll(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func runConnectWithWait(ctx context.Context, root string, options cloudCLIOptions, store TokenStore, stdout, stderr io.Writer, wait func(context.Context, time.Duration) error) int {
	config, _, err := ReadConfig(root)
	if err != nil {
		return renderCLIError(err, stderr)
	}
	client, err := NewCloudClient(options.baseURL, nil)
	if err != nil {
		return renderCLIError(err, stderr)
	}
	session, err := client.CreateAuthorizationSession(ctx, config.App.BundleID)
	if err != nil {
		return renderCLIError(err, stderr)
	}
	browserURL := session.VerificationURIComplete
	if browserURL == "" {
		browserURL = session.VerificationURI
	}
	fmt.Fprintf(stdout, "Authorize AttributionKit in your browser: %s\n", browserURL)
	fmt.Fprintf(stdout, "If prompted, enter code: %s\n", session.UserCode)
	if !options.noBrowser {
		if err := openBrowser(browserURL); err != nil {
			fmt.Fprintln(stderr, "Could not open a browser automatically; use the URL above:", err)
		}
	}
	expiresAt, err := time.Parse(time.RFC3339, session.ExpiresAt)
	if err != nil {
		return renderCLIError(errors.New("hosted API returned an invalid authorization expiry"), stderr)
	}
	interval := time.Duration(session.PollIntervalSeconds) * time.Second
	var exchange AuthorizationExchange
	var authorized bool
	for {
		if time.Now().Add(interval).After(expiresAt) {
			return renderCLIError(errors.New("browser authorization expired; run `attribution connect` again"), stderr)
		}
		if err := wait(ctx, interval); err != nil {
			return renderCLIError(err, stderr)
		}
		exchange, authorized, err = client.ExchangeAuthorization(ctx, session.AuthorizationSessionID, session.DeviceCode)
		if err != nil {
			return renderCLIError(err, stderr)
		}
		if authorized {
			break
		}
		if exchange.Status == "slow_down" {
			retryAfter := time.Duration(exchange.RetryAfterSeconds) * time.Second
			if retryAfter > interval {
				interval = retryAfter
			}
		}
	}
	link, err := client.LinkApplication(ctx, exchange.AccessToken, config.App.BundleID)
	if err != nil {
		return renderCLIError(err, stderr)
	}
	if link.ApplicationID != exchange.ApplicationID || link.OrganizationID != exchange.OrganizationID {
		return renderCLIError(errors.New("authorization and application-link identities did not match"), stderr)
	}
	credentialRef, normalizedBaseURL, err := cloudCredentialReference(client.BaseURL, link.OrganizationID, link.ApplicationID, link.BundleID)
	if err != nil {
		return renderCLIError(err, stderr)
	}
	if err := store.Set(credentialRef, exchange.AccessToken); err != nil {
		return renderCLIError(err, stderr)
	}
	binding := CloudBinding{
		SchemaVersion:  cloudBindingVersion,
		BaseURL:        normalizedBaseURL,
		OrganizationID: link.OrganizationID,
		ApplicationID:  link.ApplicationID,
		BundleID:       link.BundleID,
		CredentialRef:  credentialRef,
	}
	if err := WriteCloudBinding(root, binding); err != nil {
		return renderCLIError(err, stderr)
	}
	fmt.Fprintf(stdout, "Connected %s to application %s.\n", link.BundleID, link.ApplicationID)
	fmt.Fprintf(stdout, "Wrote non-secret binding to %s; access token is in the OS keychain.\n", CloudConfigPath)
	return 0
}

func printCloudUsage(writer io.Writer, command string) {
	switch command {
	case "connect":
		fmt.Fprintln(writer, "usage: attribution connect [--project <dir>] [--api-base <url>] [--no-browser]")
	case "runs upload":
		fmt.Fprintln(writer, "usage: attribution runs upload [--project <dir>]")
	case "ping":
		fmt.Fprintln(writer, "usage: attribution ping [--project <dir>]")
	case "live-check":
		fmt.Fprintln(writer, "usage: attribution live-check [--project <dir>] [--json]")
	}
}

func openBrowser(target string) error {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.Command("open", target)
	case "windows":
		command = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	default:
		command = exec.Command("xdg-open", target)
	}
	return command.Start()
}

func renderLiveStatus(writer io.Writer, status LiveStatus) {
	fmt.Fprintf(writer, "Live status for application %s\n", status.ApplicationID)
	for _, section := range []string{"config", "build", "your-logic", "device", "production"} {
		result := status.Sections[section]
		fmt.Fprintf(writer, "  %-10s %s [evidence: %s · basis: %s]\n", section, result.Status, result.Evidence, result.Basis)
	}
	if status.ProductionEvidence {
		fmt.Fprintln(writer, "Production evidence is backed by a verified Apple receipt.")
	} else {
		fmt.Fprintln(writer, "Production remains open until a real Apple postback is cryptographically verified.")
	}
}

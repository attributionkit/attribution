package attribution

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Version             int                 `yaml:"version" json:"version"`
	Mode                string              `yaml:"mode" json:"mode"`
	App                 AppConfig           `yaml:"app" json:"app"`
	ConversionAuthority ConversionAuthority `yaml:"conversionAuthority" json:"conversionAuthority"`
	EventTransports     []string            `yaml:"eventTransports" json:"eventTransports"`
	Providers           ProvidersConfig     `yaml:"providers" json:"providers"`
	Schema              EventSchema         `yaml:"schema" json:"schema"`
}

type AppConfig struct {
	BundleID string `yaml:"bundleId" json:"bundleId"`
}

type ConversionAuthority struct {
	Owner string `yaml:"owner" json:"owner"`
}

type ProvidersConfig struct {
	Apple AppleProviderConfig `yaml:"apple" json:"apple"`
	Meta  *MetaProviderConfig `yaml:"meta,omitempty" json:"meta,omitempty"`
}

type AppleProviderConfig struct {
	Endpoint       string   `yaml:"endpoint" json:"endpoint"`
	SKAdNetworkIDs []string `yaml:"skAdNetworkIds" json:"skAdNetworkIds"`
}

type MetaProviderConfig struct {
	AppID string `yaml:"appId" json:"appId"`
}

type EventSchema struct {
	Events []string `yaml:"events" json:"events"`
}

var (
	bundleIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.-]*[A-Za-z0-9]$`)
	skadIDPattern   = regexp.MustCompile(`^[a-z0-9]+\.skadnetwork$`)
	metaIDPattern   = regexp.MustCompile(`^[0-9]{5,20}$`)
)

func ReadConfig(root string) (Config, []byte, error) {
	if err := validateSafeTarget(root, ConfigPath); err != nil {
		return Config{}, nil, err
	}
	path := filepath.Join(root, filepath.FromSlash(ConfigPath))
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Config{}, nil, &MissingConfigError{Root: root}
	}
	if err != nil {
		return Config{}, nil, fmt.Errorf("read %s: %w", ConfigPath, err)
	}
	config, err := ParseConfig(raw)
	return config, raw, err
}

func ParseConfig(raw []byte) (Config, error) {
	var config Config
	if problems := validateRequiredConfigShape(raw); len(problems) > 0 {
		return Config{}, &ConfigValidationError{Problems: problems}
	}
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if err := decoder.Decode(&config); err != nil {
		return Config{}, &ConfigValidationError{Problems: []string{err.Error()}}
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return Config{}, &ConfigValidationError{Problems: []string{"multiple YAML documents are not supported"}}
		}
		return Config{}, &ConfigValidationError{Problems: []string{err.Error()}}
	}
	if problems := validateConfig(config); len(problems) > 0 {
		return Config{}, &ConfigValidationError{Problems: problems}
	}
	return config, nil
}

func validateConfig(config Config) []string {
	var problems []string
	if config.Version != 1 {
		problems = append(problems, "version must be 1")
	}
	if config.Mode != "managed" && config.Mode != "external" {
		problems = append(problems, `mode must be "managed" or "external"`)
	}
	if !bundleIDPattern.MatchString(config.App.BundleID) || !strings.Contains(config.App.BundleID, ".") {
		problems = append(problems, "app.bundleId must be a non-empty reverse-DNS bundle identifier")
	}
	owner := strings.TrimSpace(config.ConversionAuthority.Owner)
	if owner == "" {
		problems = append(problems, "conversionAuthority.owner is required")
	}
	if config.Mode == "managed" && owner != "managed-runtime" {
		problems = append(problems, `managed mode requires conversionAuthority.owner to be "managed-runtime"`)
	}
	if config.Mode == "external" && normalizeName(owner) == normalizeName("managed-runtime") {
		problems = append(problems, "external mode requires an installed external conversion authority")
	}

	seenTransports := make(map[string]struct{}, len(config.EventTransports))
	for i, transport := range config.EventTransports {
		transport = strings.TrimSpace(transport)
		if transport == "" {
			problems = append(problems, fmt.Sprintf("eventTransports[%d] must not be empty", i))
			continue
		}
		normalized := normalizeName(transport)
		if _, found := seenTransports[normalized]; found {
			problems = append(problems, fmt.Sprintf("eventTransports contains duplicate %q", transport))
		}
		seenTransports[normalized] = struct{}{}
		if normalized == normalizeName(owner) {
			problems = append(problems, fmt.Sprintf("%q cannot be both conversionAuthority.owner and an event transport", transport))
		}
	}

	parsedEndpoint, err := url.ParseRequestURI(config.Providers.Apple.Endpoint)
	if err != nil || parsedEndpoint.Scheme != "https" || parsedEndpoint.Host == "" {
		problems = append(problems, "providers.apple.endpoint must be an absolute https URL")
	} else if parsedEndpoint.User != nil || parsedEndpoint.RawQuery != "" || parsedEndpoint.ForceQuery || parsedEndpoint.Fragment != "" {
		problems = append(problems, "providers.apple.endpoint must not contain credentials, a query, or a fragment")
	}

	seenSKAd := make(map[string]struct{}, len(config.Providers.Apple.SKAdNetworkIDs))
	for i, id := range config.Providers.Apple.SKAdNetworkIDs {
		if !skadIDPattern.MatchString(id) {
			problems = append(problems, fmt.Sprintf("providers.apple.skAdNetworkIds[%d] must end in .skadnetwork and contain lowercase letters or digits", i))
		}
		if _, found := seenSKAd[id]; found {
			problems = append(problems, fmt.Sprintf("providers.apple.skAdNetworkIds contains duplicate %q", id))
		}
		seenSKAd[id] = struct{}{}
	}

	if config.Providers.Meta != nil {
		appID := strings.TrimSpace(config.Providers.Meta.AppID)
		if !metaIDPattern.MatchString(appID) {
			problems = append(problems, "providers.meta.appId must contain 5 to 20 digits")
		} else if isPlaceholderMetaAppID(appID) {
			problems = append(problems, "providers.meta.appId is a placeholder; enter the real Meta app id")
		}
	}
	if config.Mode == "external" && metaIsAuthority(config) && config.Providers.Meta == nil {
		problems = append(problems, "Meta conversion authority requires providers.meta.appId so the compiled plugin can explicitly enable Meta conversion reporting")
	}

	if len(config.Schema.Events) == 0 {
		problems = append(problems, "schema.events must contain at least one event")
	}
	seenEvents := make(map[string]struct{}, len(config.Schema.Events))
	for i, event := range config.Schema.Events {
		if _, known := knownEvents[event]; !known {
			problems = append(problems, fmt.Sprintf("schema.events[%d] %q is not a known v0.1 event (install, trial, purchase, retention)", i, event))
		}
		if _, found := seenEvents[event]; found {
			problems = append(problems, fmt.Sprintf("schema.events contains duplicate %q", event))
		}
		seenEvents[event] = struct{}{}
	}
	return problems
}

func validateRequiredConfigShape(raw []byte) []string {
	var document yaml.Node
	if err := yaml.Unmarshal(raw, &document); err != nil {
		// The typed decoder will return the useful syntax error.
		return nil
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return []string{"desired state must be a YAML object"}
	}
	root := document.Content[0]
	var problems []string
	required := [][]string{
		{"version"},
		{"mode"},
		{"app"}, {"app", "bundleId"},
		{"conversionAuthority"}, {"conversionAuthority", "owner"},
		{"eventTransports"},
		{"providers"}, {"providers", "apple"},
		{"providers", "apple", "endpoint"},
		{"providers", "apple", "skAdNetworkIds"},
		{"schema"}, {"schema", "events"},
	}
	for _, path := range required {
		node := yamlNodeAt(root, path)
		if node == nil || node.Tag == "!!null" {
			problems = append(problems, strings.Join(path, ".")+" is required and must not be null")
		}
	}
	return problems
}

func yamlNodeAt(node *yaml.Node, path []string) *yaml.Node {
	current := node
	for _, wanted := range path {
		if current == nil || current.Kind != yaml.MappingNode {
			return nil
		}
		var next *yaml.Node
		for i := 0; i+1 < len(current.Content); i += 2 {
			if current.Content[i].Value == wanted {
				next = current.Content[i+1]
				break
			}
		}
		current = next
	}
	return current
}

func isPlaceholderMetaAppID(appID string) bool {
	return appID == "" || strings.Trim(appID, "0") == ""
}

func normalizeName(value string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(value) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func managerMatchesOwner(manager conversionManager, owner string) bool {
	normalizedOwner := normalizeName(owner)
	candidates := append([]string{manager.Name, manager.Package}, manager.Aliases...)
	for _, candidate := range candidates {
		if normalizeName(candidate) == normalizedOwner {
			return true
		}
	}
	return false
}

func schemaHash(config Config) string {
	payload := struct {
		Events []string `json:"events"`
	}{Events: config.Schema.Events}
	raw, _ := json.Marshal(payload)
	return sha256Hex(raw)
}

func sha256Hex(raw []byte) string {
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

package attribution

import (
	"strings"
	"testing"
)

func TestParseConfigAcceptsPublicShape(t *testing.T) {
	config, err := ParseConfig([]byte(validConfigYAML()))
	if err != nil {
		t.Fatal(err)
	}
	if config.Mode != "managed" || config.Providers.Meta.AppID != "987654321" {
		t.Fatalf("unexpected config: %#v", config)
	}
	if got := schemaHash(config); got != "3d8ac4b627c9ac1bfdbe44231ce4261f3a31eaa5f784c52f0f29d48a52481074" {
		t.Fatalf("schema hash changed: %s", got)
	}
}

func TestParseConfigRejectsDuplicateAndUnknownEvents(t *testing.T) {
	duplicate := strings.Replace(validConfigYAML(), "    - trial\n", "    - install\n", 1)
	_, err := ParseConfig([]byte(duplicate))
	if err == nil || !strings.Contains(err.Error(), `duplicate "install"`) {
		t.Fatalf("expected duplicate event error, got %v", err)
	}
	unknown := strings.Replace(validConfigYAML(), "    - trial", "    - signup", 1)
	_, err = ParseConfig([]byte(unknown))
	if err == nil || !strings.Contains(err.Error(), "not a known v0.1 event") {
		t.Fatalf("expected unknown event error, got %v", err)
	}
}

func TestParseConfigRejectsPlaceholderMetaIDs(t *testing.T) {
	for _, placeholder := range []string{"00000", "0000000000"} {
		raw := strings.Replace(validConfigYAML(), "987654321", placeholder, 1)
		_, err := ParseConfig([]byte(raw))
		if err == nil || !strings.Contains(err.Error(), "placeholder") {
			t.Errorf("%s: expected placeholder rejection, got %v", placeholder, err)
		}
	}
}

func TestParseConfigRequiresMetaAppIDForExternalMetaAuthority(t *testing.T) {
	raw := strings.Replace(validConfigYAML(), "mode: managed", "mode: external", 1)
	raw = strings.Replace(raw, "owner: managed-runtime", "owner: Meta", 1)
	raw = strings.Replace(raw, "  meta:\n    appId: \"987654321\"\n", "", 1)
	if _, err := ParseConfig([]byte(raw)); err == nil || !strings.Contains(err.Error(), "requires providers.meta.appId") {
		t.Fatalf("expected Meta app id requirement, got %v", err)
	}
}

func TestParseConfigRejectsInsecureEndpointAndUnknownFields(t *testing.T) {
	insecure := strings.Replace(validConfigYAML(), "https://attribution.sh", "http://attribution.sh", 1)
	if _, err := ParseConfig([]byte(insecure)); err == nil || !strings.Contains(err.Error(), "https") {
		t.Fatalf("expected https error, got %v", err)
	}
	unknown := validConfigYAML() + "surprise: true\n"
	if _, err := ParseConfig([]byte(unknown)); err == nil || !strings.Contains(err.Error(), "field surprise not found") {
		t.Fatalf("expected strict YAML error, got %v", err)
	}
}

func TestParseConfigRejectsAuthorityTransportConflation(t *testing.T) {
	raw := strings.Replace(validConfigYAML(), "eventTransports: []", "eventTransports:\n  - managed-runtime", 1)
	if _, err := ParseConfig([]byte(raw)); err == nil || !strings.Contains(err.Error(), "both conversionAuthority.owner") {
		t.Fatalf("expected conflation error, got %v", err)
	}
}

func TestParseConfigRequiresEmptyArraysToBeExplicit(t *testing.T) {
	missingTransports := strings.Replace(validConfigYAML(), "eventTransports: []\n", "", 1)
	if _, err := ParseConfig([]byte(missingTransports)); err == nil || !strings.Contains(err.Error(), "eventTransports is required") {
		t.Fatalf("expected required eventTransports error, got %v", err)
	}
	nullSKAd := strings.Replace(validConfigYAML(), "    skAdNetworkIds:\n      - cstr6suwn9.skadnetwork", "    skAdNetworkIds: null", 1)
	if _, err := ParseConfig([]byte(nullSKAd)); err == nil || !strings.Contains(err.Error(), "skAdNetworkIds is required") {
		t.Fatalf("expected non-null skAdNetworkIds error, got %v", err)
	}
}

func TestParseConfigRequiresPublisherModeForSourceIdentifiers(t *testing.T) {
	missing := strings.Replace(validConfigYAML(), "    publisherMode: true\n", "", 1)
	if _, err := ParseConfig([]byte(missing)); err == nil || !strings.Contains(err.Error(), "publisherMode is required") {
		t.Fatalf("expected required publisherMode error, got %v", err)
	}
	advertiser := strings.Replace(validConfigYAML(), "    publisherMode: true", "    publisherMode: false", 1)
	if _, err := ParseConfig([]byte(advertiser)); err == nil || !strings.Contains(err.Error(), "advertised apps") {
		t.Fatalf("expected advertiser/source-app boundary error, got %v", err)
	}
}

func TestParseConfigValidatesAssociatedDomains(t *testing.T) {
	missing := strings.Replace(validConfigYAML(), "    associatedDomains:\n      - attribution.sh\n", "", 1)
	if _, err := ParseConfig([]byte(missing)); err == nil || !strings.Contains(err.Error(), "associatedDomains is required") {
		t.Fatalf("expected required associatedDomains error, got %v", err)
	}
	invalid := strings.Replace(validConfigYAML(), "      - attribution.sh", "      - https://attribution.sh/path", 1)
	if _, err := ParseConfig([]byte(invalid)); err == nil || !strings.Contains(err.Error(), "DNS hostname") {
		t.Fatalf("expected domain validation error, got %v", err)
	}
}

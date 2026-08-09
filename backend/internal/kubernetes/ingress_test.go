package kubernetes

import (
	"strings"
	"testing"
)

func TestBuildIngressHost(t *testing.T) {
	tests := []struct {
		name   string
		app    string
		scope  string
		domain string
		want   string
	}{
		{"documented pattern", "backend-api", "acme", "idp.local", "backend-api.acme.idp.local"},
		{"empty domain falls back", "api", "acme", "", "api.acme." + DefaultIngressDomain},
		{"custom domain", "api", "acme", "apps.corp.example", "api.acme.apps.corp.example"},
		{"uppercase is lowered", "API", "ACME", "IDP.LOCAL", "api.acme.idp.local"},
		{"whitespace trimmed", "  api  ", " acme ", " idp.local ", "api.acme.idp.local"},
		// A namespace with no project must still get a usable hostname rather
		// than one containing an empty label like "api..idp.local".
		{"no scope collapses to two labels", "api", "", "idp.local", "api.idp.local"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := BuildIngressHost(tt.app, tt.scope, tt.domain)
			if err != nil {
				t.Fatalf("BuildIngressHost(%q, %q, %q) error: %v", tt.app, tt.scope, tt.domain, err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildIngressHostRejectsUnusableNames(t *testing.T) {
	tests := []struct {
		name  string
		app   string
		scope string
	}{
		{"empty app", "", "acme"},
		{"app with underscore", "back_end", "acme"},
		{"app with dot", "back.end", "acme"},
		{"scope with underscore", "api", "my_project"},
		{"app starting with hyphen", "-api", "acme"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, err := BuildIngressHost(tt.app, tt.scope, "idp.local"); err == nil {
				t.Errorf("got %q, want error", got)
			}
		})
	}
}

func TestValidateHostname(t *testing.T) {
	valid := map[string]string{
		"app.acme.idp.local":  "app.acme.idp.local",
		"APP.ACME.IDP.LOCAL":  "app.acme.idp.local",
		"app.example.com.":    "app.example.com",
		"  app.example.com  ": "app.example.com",
		"a-b.c-d.example":     "a-b.c-d.example",
	}
	for input, want := range valid {
		got, err := ValidateHostname(input)
		if err != nil {
			t.Errorf("ValidateHostname(%q) error: %v", input, err)
			continue
		}
		if got != want {
			t.Errorf("ValidateHostname(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestValidateHostnameRejectsBadInput(t *testing.T) {
	// Each of these is a realistic paste accident. The API server rejects them
	// with a message that never mentions the offending characters.
	invalid := []string{
		"",
		"   ",
		"https://app.example.com",
		"app.example.com:8080",
		"app.example.com/path",
		"user@app.example.com",
		"app example.com",
		"nodots",
		"app..example.com",
		"-app.example.com",
		"app.example.com-",
		"app_name.example.com",
	}

	for _, host := range invalid {
		if got, err := ValidateHostname(host); err == nil {
			t.Errorf("ValidateHostname(%q) = %q, want error", host, got)
		}
	}
}

func TestValidateHostnameEnforcesLabelAndTotalLength(t *testing.T) {
	if _, err := ValidateHostname(strings.Repeat("a", 64) + ".example.com"); err == nil {
		t.Error("accepted a 64-character label; DNS caps a label at 63")
	}
	if _, err := ValidateHostname(strings.Repeat("abcdef.", 40) + "example.com"); err == nil {
		t.Error("accepted a hostname over 253 characters")
	}
}

func TestIngressConfigNormalize(t *testing.T) {
	got := IngressConfig{}.Normalize()
	if got.Domain != DefaultIngressDomain {
		t.Errorf("Domain = %q, want %q", got.Domain, DefaultIngressDomain)
	}
	if got.Class != DefaultIngressClass {
		t.Errorf("Class = %q, want %q", got.Class, DefaultIngressClass)
	}

	custom := IngressConfig{Domain: "  APPS.CORP  ", Class: " traefik "}.Normalize()
	if custom.Domain != "apps.corp" || custom.Class != "traefik" {
		t.Errorf("Normalize did not clean custom values: %+v", custom)
	}
}

func TestIngressConfigScheme(t *testing.T) {
	// A .local domain with no certificate authority behind it must advertise
	// http, or every generated link lands on a browser warning.
	if got := (IngressConfig{}).Scheme(); got != "http" {
		t.Errorf("Scheme without TLS = %q, want http", got)
	}
	if got := (IngressConfig{TLSSecretName: "wildcard-tls"}).Scheme(); got != "https" {
		t.Errorf("Scheme with TLS = %q, want https", got)
	}
}

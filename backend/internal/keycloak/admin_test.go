package keycloak

import "testing"

func TestParseIssuer(t *testing.T) {
	base, realm := ParseIssuer("http://localhost:8080/realms/idp")
	if base != "http://localhost:8080" || realm != "idp" {
		t.Fatalf("got base=%q realm=%q", base, realm)
	}
	base, realm = ParseIssuer("http://localhost:8080/realms/idp/")
	if base != "http://localhost:8080" || realm != "idp" {
		t.Fatalf("trailing slash: base=%q realm=%q", base, realm)
	}
}

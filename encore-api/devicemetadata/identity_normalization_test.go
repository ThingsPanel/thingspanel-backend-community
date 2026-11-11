package devicemetadata

import "testing"

func TestNormalizePrincipalID_UUIDPassthrough(t *testing.T) {
	raw := "550e8400-e29b-41d4-a716-446655440000"
	got := normalizePrincipalID(raw)
	if got != raw {
		t.Fatalf("normalizePrincipalID(%q) = %q, want %q", raw, got, raw)
	}
}

func TestNormalizePrincipalID_TextFallback(t *testing.T) {
	raw := "default"
	got := normalizePrincipalID(raw)
	want := "db7d71ef-3c6c-5382-963c-4267c46724f7"
	if got != want {
		t.Fatalf("normalizePrincipalID(%q) = %q, want %q", raw, got, want)
	}
}

func TestNormalizePrincipalID_RetainsNonHexCharacters(t *testing.T) {
	raw := "tenant-admin"
	got := normalizePrincipalID(raw)
	want := "989d538a-3e7f-5dab-a1a5-af8406c517db"
	if got != want {
		t.Fatalf("normalizePrincipalID(%q) = %q, want %q", raw, got, want)
	}
}

func TestNormalizePrincipalID_MatchesExistingModelOwner(t *testing.T) {
	if got := normalizePrincipalID("63a4b529"); got != "48c68fbd-b8b8-5c24-b524-a67edbd2d6e7" {
		t.Fatalf("device and model tenant identity differ: %s", got)
	}
}

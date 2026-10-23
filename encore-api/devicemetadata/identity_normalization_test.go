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
	want := "defa0000-0000-0000-0000-000000000000"
	if got != want {
		t.Fatalf("normalizePrincipalID(%q) = %q, want %q", raw, got, want)
	}
}

func TestNormalizePrincipalID_HexEncoding(t *testing.T) {
	raw := "tenant-admin"
	got := normalizePrincipalID(raw)
	want := "eaad0000-0000-0000-0000-000000000000"
	if got != want {
		t.Fatalf("normalizePrincipalID(%q) = %q, want %q", raw, got, want)
	}
}

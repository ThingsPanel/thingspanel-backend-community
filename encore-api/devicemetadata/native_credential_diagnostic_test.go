package devicemetadata

import "testing"

func TestNativeCredentialDiagnosticStates(t *testing.T) {
	cases := []struct {
		name        string
		c           nativeCredentialDiagnosticCounts
		code, state string
	}{
		{"never", nativeCredentialDiagnosticCounts{}, "NO_CREDENTIAL", "unknown"},
		{"revoked", nativeCredentialDiagnosticCounts{total: 3, revoked: 3}, "CREDENTIAL_REVOKED", "fail"},
		{"expired", nativeCredentialDiagnosticCounts{total: 2, expired: 2}, "CREDENTIAL_EXPIRED", "fail"},
		{"mixed unusable", nativeCredentialDiagnosticCounts{total: 2, revoked: 1, expired: 1}, "CREDENTIAL_UNAVAILABLE", "fail"},
		{"replacement pending", nativeCredentialDiagnosticCounts{total: 2, revoked: 1, pending: 1}, "CREDENTIAL_PENDING", "unknown"},
		{"replacement active", nativeCredentialDiagnosticCounts{total: 2, revoked: 1, active: 1}, "CREDENTIAL_ACTIVE", "pass"},
		{"unknown lifecycle", nativeCredentialDiagnosticCounts{total: 1}, "CREDENTIAL_UNAVAILABLE", "unknown"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := credentialDiagnosticCheck(tt.c)
			if got.Code != tt.code || got.State != tt.state {
				t.Fatalf("%+v", got)
			}
		})
	}
}

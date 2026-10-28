package ports

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPolicySnapshotResolution(t *testing.T) {
	for _, tc := range []struct {
		name, alias, body string
		status            int
		wantErr           bool
	}{
		{"default", "", `{"configured":true,"policy_id":"native-a","group_revision":7}`, 200, false},
		{"explicit", "alias-a", `{"configured":true,"policy_id":"native-a","group_revision":7}`, 200, false},
		{"no-default", "", `{"configured":false}`, 200, false},
		{"explicit-no-fallback", "alias-a", `{"configured":false}`, 200, true},
		{"unavailable", "", `{}`, 409, true},
		{"missing-revision", "", `{"configured":true,"policy_id":"native-a"}`, 200, true},
		{"invalid-response", "", `{}`, 200, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if req.URL.Path != "/api/v1/notification/source-policies/tenant-a" || req.URL.Query().Get("alias") != tc.alias {
					t.Error("wrong policy identity")
				}
				if req.Header.Get("Authorization") != "Bearer reader-secret" || req.Header.Get("X-Notification-Deployment") != "dep-a" {
					t.Error("missing scoped credentials")
				}
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer srv.Close()
			owner := NewHTTPNotificationOwner(Config{CommunityBaseURL: srv.URL, SourceReaderToken: "reader-secret", DeploymentID: "dep-a"})
			snapshot, err := owner.ResolvePolicy(context.Background(), "tenant-a", tc.alias)
			if (err != nil) != tc.wantErr {
				t.Fatalf("snapshot=%+v err=%v", snapshot, err)
			}
			if !tc.wantErr && snapshot.Configured && snapshot.GroupRevision != 7 {
				t.Fatal("revision was not frozen")
			}
		})
	}
}

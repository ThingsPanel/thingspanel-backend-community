package brokercontrol

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	bc "project/internal/brokercontract"
	"strings"
	"testing"
	"time"
)

type leaseProtocolRepository struct {
	*fakeRepository
	acks int
}

func (r *leaseProtocolRepository) RecordCredentialLease(context.Context, bc.AuthRequest, bc.Grant) error {
	return nil
}
func (r *leaseProtocolRepository) CredentialLeasesReady(context.Context) error { return nil }
func (r *leaseProtocolRepository) AcknowledgeCredentialLease(context.Context, bc.CredentialLeaseAck) error {
	r.acks++
	return nil
}
func TestCredentialLeaseEndpointsRequireInstalledBrokerIdentity(t *testing.T) {
	repo := &leaseProtocolRepository{fakeRepository: &fakeRepository{}}
	owner, err := NewOwner(repo, Config{Tokens: []TokenBinding{{Token: testToken(), BrokerCell: "community", BrokerInstance: "node-1"}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/v1/credential-leases/ready", "/v1/credential-leases/ack"} {
		for _, valid := range []bool{false, true} {
			body := map[string]any{"broker_cell": "community", "broker_instance": "node-1"}
			if path == "/v1/credential-leases/ack" {
				body["connection_id"] = "connection-a"
				body["expires_at"] = time.Now().Add(time.Minute)
			}
			raw, _ := json.Marshal(body)
			req := httptest.NewRequest("POST", path, strings.NewReader(string(raw)))
			token := testToken()
			if !valid {
				token = strings.Repeat("z", 32)
			}
			req.Header.Set("Authorization", "Bearer "+token)
			res := httptest.NewRecorder()
			owner.Handler().ServeHTTP(res, req)
			if !valid && res.Code != 401 {
				t.Fatal("unauthenticated lease control accepted")
			}
			if valid && res.Code != 200 && res.Code != 204 {
				t.Fatalf("authorized endpoint failed: %d", res.Code)
			}
		}
	}
	if repo.acks != 1 {
		t.Fatal("unauthenticated ACK reached repository")
	}
}

package offlineupgrade

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"encore.app/devicemetadata/credentialsecret"
	"gorm.io/gorm"
)

func credentialFixture() CredentialInput {
	d := fixtureDevices()
	return CredentialInput{SourceDatabase: d.SourceDatabase, TargetDatabase: d.TargetDatabase, SourceArchiveSHA256: d.SourceArchiveSHA256, TargetArchiveSHA256: d.TargetArchiveSHA256, QuarantineSHA256: d.QuarantineSHA256, RecordedAt: time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC)}
}
func credentialDevice(id, tenant, voucher string) json.RawMessage {
	raw, _ := json.Marshal(map[string]any{"id": id, "tenant_id": tenant, "voucher": voucher, "access_way": "A"})
	return raw
}
func TestLegacyMQTTCredentialParsingPreservesExactSecret(t *testing.T) {
	for _, test := range []struct{ raw, kind, secret, mode string }{
		{`{"username":"device","password":" secret with spaces "}`, "BASIC", " secret with spaces ", "username_password"},
		{`{"username":"token-original","password":""}`, "ACCESSTOKEN", "token-original", "token"},
	} {
		secret, user, mode, err := parseLegacyMQTTVoucher(test.raw, test.kind)
		if err != nil || string(secret) != test.secret || mode != test.mode {
			t.Fatal("credential was replaced or misinterpreted")
		}
		if mode == "token" && user != nil {
			t.Fatal("token exposed as public identifier")
		}
	}
	for _, raw := range []string{`{"username":"x","username":"y","password":"s"}`, `{"username":"x","password":"s","extra":"keep-me"}`, `{"username":"x"}`, `{"username":"x","password":null}`, `{"username":"x","password":""}`, `{"username":"x","password":"secret"} {}`} {
		if _, _, _, err := parseLegacyMQTTVoucher(raw, "BASIC"); err == nil {
			t.Fatal("ambiguous source voucher accepted")
		}
	}
}
func TestCredentialPlanSeparatesProtocolsAndHidesSecrets(t *testing.T) {
	in := credentialFixture()
	in.Devices = []json.RawMessage{credentialDevice("a", "tenant", `{"username":"user","password":"unique-private-secret"}`), json.RawMessage(`{"id":"b","tenant_id":"tenant","protocol":"HTTP","voucher":"{}"}`), json.RawMessage(`{"id":"c","tenant_id":"tenant","service_access_id":"service","voucher":"{}"}`)}
	plan, err := PlanCredentials(in)
	if err != nil || plan.Selected != 1 || plan.Pending["non_mqtt_protocol"] != 1 || plan.Pending["service_connector"] != 1 {
		t.Fatalf("unexpected plan err=%v", err)
	}
	encoded, _ := json.Marshal(plan)
	if bytes.Contains(encoded, []byte("unique-private-secret")) || bytes.Contains(encoded, []byte("username")) {
		t.Fatal("plan exported protected material")
	}
	in.ExcludedDevices = []string{"absent"}
	if _, err := PlanCredentials(in); err == nil {
		t.Fatal("stale quarantine accepted")
	}
	in.ExcludedDevices = nil
	in.Devices = append(in.Devices, in.Devices[0])
	if _, err := PlanCredentials(in); err == nil {
		t.Fatal("duplicate source credential accepted")
	}
}

func verifyCredentialRehearsal(t *testing.T, db *gorm.DB) {
	t.Helper()
	in := credentialFixture()
	in.Devices = []json.RawMessage{credentialDevice("one", "tenant", `{"username":"one","password":"secret-one"}`), credentialDevice("two", "tenant", `{"username":"blocked","password":"secret-two"}`)}
	protector, err := credentialsecret.NewCredentialSecretProtector("fixture", map[string][]byte{"fixture": bytes.Repeat([]byte{1}, 32)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("ALTER TABLE device_credentials ADD CONSTRAINT fixture_credential_failure CHECK (public_identifier <> 'blocked')").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyCredentialsPG(context.Background(), db, in, protector); err == nil {
		t.Fatal("constraint should roll back full batch")
	}
	var count int64
	db.Table("device_credentials").Count(&count)
	if count != 0 {
		t.Fatal("partial credential conversion survived rollback")
	}
	in.Devices[1] = credentialDevice("two", "tenant", `{"username":"two","password":"secret-two"}`)
	first, err := ApplyCredentialsPG(context.Background(), db, in, protector)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ApplyCredentialsPG(context.Background(), db, in, protector)
	if err != nil || first.ConvertedCredentials != 2 || second.State != "verified-existing-rehearsal-credentials" || second.MigrationComplete {
		t.Fatalf("retry failed: %v", err)
	}
	in.Devices[1] = credentialDevice("two", "tenant", `{"username":"two","password":"changed-secret"}`)
	if _, err := ApplyCredentialsPG(context.Background(), db, in, protector); err == nil {
		t.Fatal("source secret drift accepted")
	}
	in.Devices[1] = credentialDevice("two", "tenant", `{"username":"two","password":"secret-two"}`)
	other, _ := credentialsecret.NewCredentialSecretProtector("fixture", map[string][]byte{"fixture": bytes.Repeat([]byte{2}, 32)})
	if _, err := ApplyCredentialsPG(context.Background(), db, in, other); err == nil {
		t.Fatal("wrong key accepted")
	}
	in.Devices[1] = credentialDevice("two", "other-tenant", `{"username":"two","password":"secret-two"}`)
	if _, err := ApplyCredentialsPG(context.Background(), db, in, protector); err == nil {
		t.Fatal("cross-tenant credential owner accepted")
	}
	var events []auditEvent
	db.Table("metadata_audit_events").Where("operation_name = ?", "offline.device.credential.convert").Find(&events)
	for _, event := range events {
		if strings.Contains(event.RedactedChanges, "secret-") {
			t.Fatal("plaintext leaked into audit")
		}
	}
	var state string
	db.Table("devices").Select("administrative_state").Where("name = ?", "one").Scan(&state)
	if state != "disabled" {
		t.Fatal("migration enabled a device")
	}
	db.Table("device_access_bindings").Count(&count)
	if count != 0 {
		t.Fatal("credential stage fabricated access bindings")
	}
}

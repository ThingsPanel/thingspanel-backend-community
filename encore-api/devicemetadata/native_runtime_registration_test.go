package devicemetadata

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNativeRuntimeRegistrationRequiresOwnerConfirmation(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenant := testTenantID()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	id := insertCredentialSchemaDevice(t, ctx, tx, tenant)
	if _, err = tx.Exec(ctx, `UPDATE devices SET device_number=$2,administrative_state='enabled' WHERE id=$1`, id, id); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO device_access_bindings(tenant_id,device_id,direct_access,device_parameters,updated_by) VALUES($1,$2,$3::jsonb,'{}','test')`, tenant, id, validNativeDirectAccess); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	defer db.Exec(ctx, `DELETE FROM device_access_bindings WHERE tenant_id=$1 AND device_id=$2`, tenant, id)
	confirm := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+strings.Repeat("x", 32) || r.Header.Get("X-Automation-Tenant") != tenant || r.Header.Get("X-Automation-Deployment") != "dep" {
			t.Error("service identity missing")
			w.WriteHeader(401)
			return
		}
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if body["deviceId"] != id || body["voucher"] != `{"username":"secret"}` {
			t.Error("runtime registration changed identity or credential")
		}
		if !confirm {
			w.WriteHeader(409)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": map[string]string{"deviceId": id}})
	}))
	defer server.Close()
	t.Setenv("TP_COMMUNITY_BASE_URL", server.URL)
	t.Setenv("TP_AUTOMATION_SERVICE_TOKEN", strings.Repeat("x", 32))
	t.Setenv("TP_SOURCE_DEPLOYMENT_ID", "dep")
	item := runtimeOutboxItem{EventID: uuid.NewString(), TenantID: tenant, DeviceID: id}
	change := credentialChangeDesired{Change: "activation", CredentialID: uuid.NewString()}
	if err := registerNativeMQTTRuntime(ctx, item, change, `{"username":"secret"}`); err == nil {
		t.Fatal("unconfirmed owner response succeeded")
	}
	var count int
	if err := db.QueryRow(ctx, `SELECT count(*) FROM device_access_bindings WHERE tenant_id=$1 AND device_id=$2 AND runtime_namespace IS NOT NULL`, tenant, id).Scan(&count); err != nil || count != 0 {
		t.Fatal("unconfirmed runtimeRef written", err)
	}
	confirm = true
	if err := registerNativeMQTTRuntime(ctx, item, change, `{"username":"secret"}`); err != nil {
		t.Fatal(err)
	}
	if err := registerNativeMQTTRuntime(ctx, item, change, `{"username":"secret"}`); err != nil {
		t.Fatalf("confirmed registration retry failed: %v", err)
	}
	runtimeID, err := lookupRuntimeDeviceID(ctx, tenant, id)
	if err != nil || runtimeID != id {
		t.Fatal("confirmed UUID not recorded", err)
	}
}

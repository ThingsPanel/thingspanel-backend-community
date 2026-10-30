package devicemetadata

import (
	"context"
	"github.com/google/uuid"
	"strings"
	"testing"

	"encore.app/l1-access/authn"
	"encore.dev/beta/auth"
	"encore.dev/beta/errs"
	"encore.dev/et"
)

func TestAccessProtocolReadsRevisionOrDirectAccess(t *testing.T) {
	cases := []struct {
		name string
		docs [][]byte
		want string
	}{
		{"revision", [][]byte{[]byte(`{"access":{"protocol":"mqtt"}}`)}, "mqtt"},
		{"direct", [][]byte{nil, []byte(`{"protocol":"coap"}`)}, "coap"},
		{"revision wins", [][]byte{[]byte(`{"access":{"protocol":"mqtt"}}`), []byte(`{"protocol":"coap"}`)}, "mqtt"},
		{"none", [][]byte{[]byte(`{}`), []byte(`not json`), nil}, ""},
	}
	for _, c := range cases {
		if got := accessProtocol(c.docs...); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestNativeConnectionInfoReportsProtocolGatewayAndNeverSecrets(t *testing.T) {
	requireDeviceGroupPostgres(t)
	ctx := context.Background()
	tenantID, subjectID := testTenantID(), "connection-info-test"
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	deviceID := uuid.NewString()
	if err := insertNativeReadDevice(ctx, tx, tenantID, deviceID, "diag-"+deviceID, "diagnostic device", `{}`, true); err != nil {
		t.Fatalf("insert device fixture: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(ctx, `DELETE FROM device_access_bindings WHERE tenant_id=$1 AND device_id=$2::uuid`, tenantID, deviceID)
		_, _ = db.Exec(ctx, `DELETE FROM devices WHERE tenant_id=$1 AND id=$2::uuid`, tenantID, deviceID)
	})
	et.OverrideAuthInfo(auth.UID(subjectID), &authn.Data{UserID: subjectID, TenantID: tenantID, Roles: []string{"TENANT_ADMIN"}})
	defer et.OverrideAuthInfo("", nil)
	identity, err := currentNativeDeviceReadIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := readNativeConnectionInfo(ctx, identity, deviceID, "ci-unconfigured"); errs.Code(err) != errs.NotFound {
		t.Fatalf("unconfigured device error=%v, want NotFound", err)
	}
	if _, err := db.Exec(ctx, `UPDATE device_access_bindings SET direct_access='{"mode":"direct","protocol":"mqtt","password":"must-not-leak"}'::jsonb,
		configuration_generation=configuration_generation+1 WHERE tenant_id=$1 AND device_id=$2::uuid AND $3<>''`, tenantID, deviceID, subjectID); err != nil {
		t.Fatalf("insert binding fixture: %v", err)
	}
	t.Setenv(connectionInfoEndpointEnv, "")
	info, err := readNativeConnectionInfo(ctx, identity, deviceID, "ci-direct")
	if err != nil || info.Data.Protocol != "mqtt" || info.Data.Endpoint != nil || info.Data.DeviceID != deviceID {
		t.Fatalf("direct access info wrong: %+v err=%v", info, err)
	}
	t.Setenv(connectionInfoEndpointEnv, "mqtts://iot.example.test:8883")
	info, err = readNativeConnectionInfo(ctx, identity, deviceID, "ci-endpoint")
	if err != nil || info.Data.Endpoint == nil || *info.Data.Endpoint != "mqtts://iot.example.test:8883" {
		t.Fatalf("configured endpoint not reported: %+v err=%v", info, err)
	}
	if encoded := mustJSON(t, info); strings.Contains(string(encoded), "must-not-leak") {
		t.Fatal("connection info leaked access parameters")
	}
	other := nativeDeviceReadIdentity{tenantID: "00000000-0000-0000-0000-00000000dead"}
	if _, err := readNativeConnectionInfo(ctx, other, deviceID, "ci-other-tenant"); err == nil {
		t.Fatal("another tenant read this device's connection info")
	}
}

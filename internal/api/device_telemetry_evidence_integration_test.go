package api

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/spf13/viper"
	"gorm.io/gorm"
	"project/internal/model"
	"project/internal/service"
	"project/pkg/global"
	"project/pkg/utils"
)

func TestBrokerTelemetryEvidenceTrustedReceiptsAndSemanticPins(t *testing.T) {
	db, cleanupDB := openSemanticEvidencePG(t)
	defer cleanupDB()
	redisClient := startBrokerAPITestRedis(t)
	previousDB, previousRedis := global.DB, global.REDIS
	global.DB, global.REDIS = db, redisClient
	t.Cleanup(func() { global.DB, global.REDIS = previousDB, previousRedis })

	previousProfile, previousBase, previousDev, previousJWT := viper.Get("mqtt.input_profile"), viper.Get("mqtt.semantic_base_url"), viper.Get("mqtt.semantic_development_http"), viper.Get("jwt.key")
	previousHasProfile, previousHasBase, previousHasDev, previousHasJWT := viper.IsSet("mqtt.input_profile"), viper.IsSet("mqtt.semantic_base_url"), viper.IsSet("mqtt.semantic_development_http"), viper.IsSet("jwt.key")
	t.Cleanup(func() {
		service.CloseBrokerSemanticIdleConnections()
		for _, item := range []struct {
			key   string
			value any
			set   bool
		}{
			{"mqtt.input_profile", previousProfile, previousHasProfile},
			{"mqtt.semantic_base_url", previousBase, previousHasBase},
			{"mqtt.semantic_development_http", previousDev, previousHasDev},
			{"jwt.key", previousJWT, previousHasJWT},
		} {
			if item.set {
				viper.Set(item.key, item.value)
			} else {
				viper.Set(item.key, "")
			}
		}
	})

	const (
		tenant        = "telemetry-tenant-a"
		otherTenant   = "telemetry-tenant-b"
		userID        = "telemetry-user-a"
		rootID        = "telemetry-root-a"
		childID       = "telemetry-child-a"
		childBID      = "telemetry-child-b"
		foreignDevice = "telemetry-foreign-device"
		jwtKey        = "telemetry-evidence-test-key"
	)
	if err := db.Exec(`CREATE TABLE broker_ingress_receipts (id uuid PRIMARY KEY,tenant_id varchar(36) NOT NULL,device_id varchar(36) NOT NULL,stream varchar(64) NOT NULL,source_event_id varchar(256),broker_cell varchar(128) NOT NULL,route_epoch bigint NOT NULL,payload bytea NOT NULL,metadata jsonb NOT NULL,canonical_sha256 bytea NOT NULL,state varchar(16) NOT NULL,created_at timestamptz NOT NULL,processed_at timestamptz)`).Error; err != nil {
		t.Fatal("create isolated trusted receipt table")
	}
	migration, err := os.ReadFile("../../sql/broker/014_telemetry_evidence_index.sql")
	if err != nil {
		t.Fatal("read telemetry evidence index migration")
	}
	if err := db.Exec(string(migration)).Error; err != nil {
		t.Fatalf("apply telemetry evidence index migration: %v", err)
	}
	if err := db.Exec(string(migration)).Error; err != nil {
		t.Fatalf("repeat telemetry evidence index migration: %v", err)
	}
	var indexCount int64
	if err := db.Raw(`SELECT count(*) FROM pg_indexes WHERE schemaname=current_schema() AND indexname='broker_ingress_receipts_trusted_telemetry_device_time_idx'`).Scan(&indexCount).Error; err != nil || indexCount != 1 {
		t.Fatalf("trusted telemetry index count=%d err=%v", indexCount, err)
	}
	if err := db.Exec(`INSERT INTO users(id,tenant_id,status,authority) VALUES (?,?,?,?)`, userID, tenant, "N", "TENANT_ADMIN").Error; err != nil {
		t.Fatal("seed user")
	}
	root := model.Device{ID: rootID, TenantID: tenant, Name: stringPtr("root"), Voucher: `{}`, IsEnabled: "enabled", ActivateFlag: "active", DeviceNumber: "root-number", IsOnline: 0}
	child := model.Device{ID: childID, TenantID: tenant, Name: stringPtr("child a"), Voucher: `{}`, IsEnabled: "enabled", ActivateFlag: "active", DeviceNumber: "child-a-number", ParentID: stringPtr(rootID), IsOnline: 0}
	childB := model.Device{ID: childBID, TenantID: tenant, Name: stringPtr("child b"), Voucher: `{}`, IsEnabled: "enabled", ActivateFlag: "active", DeviceNumber: "child-b-number", ParentID: stringPtr(rootID), IsOnline: 0}
	if err := db.Create(&[]model.Device{root, child, childB}).Error; err != nil {
		t.Fatal("seed logical devices")
	}
	foreign := model.Device{ID: foreignDevice, TenantID: otherTenant, Name: stringPtr("foreign"), Voucher: `{}`, IsEnabled: "enabled", ActivateFlag: "active", DeviceNumber: "foreign-number", IsOnline: 0}
	if err := db.Create(&foreign).Error; err != nil {
		t.Fatal("seed foreign tenant device")
	}
	if err := db.Exec(`INSERT INTO broker_transport_bindings(tenant_id,device_id,broker_cell,route_epoch,phase) VALUES (?,?,?,2,'active')`, tenant, rootID, "telemetry-cell-new").Error; err != nil {
		t.Fatal("seed current root route")
	}
	activityTime := time.Now().UTC().Add(-time.Minute)
	activityReceipt := uuid.NewString()
	if err := db.Exec(`INSERT INTO broker_telemetry_activity(tenant_id,device_id,transport_device_id,broker_cell,route_epoch,receipt_id,observed_at,next_check_at) VALUES (?,?,?,?,?,?,?,?)`, tenant, childID, rootID, "telemetry-cell-new", 2, activityReceipt, activityTime, activityTime).Error; err != nil {
		t.Fatal("seed child route activity")
	}
	token := semanticEvidenceJWT(t, jwtKey, userID, tenant, "TENANT_ADMIN")
	if err := redisClient.Set(context.Background(), token, "1", time.Hour).Err(); err != nil {
		t.Fatal("seed live user session")
	}

	var semanticRequests atomic.Int64
	encore := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		semanticRequests.Add(1)
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/devices/" + childID:
			fmt.Fprintf(w, `{"id":%q,"tenant_id":%q,"device_template_id":"telemetry-template"}`, childID, tenant)
		case "/api/device-templates/telemetry-template":
			fmt.Fprintf(w, `{"id":"telemetry-template","tenant_id":%q,"thing_model_id":"telemetry-model","thing_model_snapshot_id":"telemetry-snapshot"}`, tenant)
		case "/api/v2/thing-models/telemetry-model/versions":
			fmt.Fprint(w, `{"items":[{"publishedRevision":7,"snapshotID":"telemetry-snapshot"}],"page":1,"pageSize":200,"total":1}`)
		case "/api/v2/thing-models/telemetry-model/versions/7":
			fmt.Fprint(w, `{"modelID":"telemetry-model","publishedRevision":7,"snapshotID":"telemetry-snapshot","model":{"name":"Weather","fields":[{"kind":"TELEMETRY","identifier":"temperature","unit":"°C","access":{"read":true,"write":false,"observe":true},"valueType":{"kind":"FLOAT"}},{"kind":"TELEMETRY","identifier":"online","access":{"read":true,"write":false,"observe":true},"valueType":{"kind":"BOOL"}},{"kind":"TELEMETRY","identifier":"humidity","unit":"%","access":{"read":true,"write":false,"observe":true},"valueType":{"kind":"FLOAT"}},{"kind":"ATTRIBUTE","identifier":"label","access":{"read":true,"write":false,"observe":true},"valueType":{"kind":"STRING"}}]},"resources":{"modelID":"telemetry-model","revision":2,"profile":{},"webChartConfig":{},"appChartConfig":{}}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer encore.Close()
	viper.Set("mqtt.input_profile", "ingress_v1")
	viper.Set("mqtt.semantic_base_url", encore.URL)
	viper.Set("mqtt.semantic_development_http", true)
	viper.Set("jwt.key", jwtKey)

	base := time.Now().UTC().Truncate(time.Second).Add(-10 * time.Minute)
	start, end := base, base.Add(time.Hour)
	payload := []byte(`{"temperature":22.5,"online":true}`)
	addReceipt := func(receiptTenant, transportDevice, logicalDevice, source, state string, epoch int64, observed time.Time, body []byte) string {
		t.Helper()
		id := uuid.NewString()
		meta, _ := json.Marshal(map[string]any{"source": source, "timestamp": observed.UnixMilli(), "logical_device_id": logicalDevice})
		digest := sha256.Sum256(body)
		processed := observed.Add(time.Second)
		if err := db.Exec(`INSERT INTO broker_ingress_receipts(id,tenant_id,device_id,stream,broker_cell,route_epoch,payload,metadata,canonical_sha256,state,created_at,processed_at) VALUES (?,?,?,'telemetry_sample','telemetry-cell-old',?,?,?::jsonb,?,?,?,?)`, id, receiptTenant, transportDevice, epoch, body, string(meta), digest[:], state, observed, processed).Error; err != nil {
			t.Fatalf("seed receipt: %v", err)
		}
		return id
	}
	wantedID := addReceipt(tenant, rootID, childID, "trusted_telemetry_batch", "processed", 1, base, payload) // old epoch remains valid history
	addReceipt(tenant, rootID, childID, "trusted_telemetry_batch", "processed", 1, end, payload)              // end is exclusive
	addReceipt(tenant, rootID, childID, "trusted_telemetry_batch", "processed", 1, base.Add(-time.Millisecond), payload)
	addReceipt(tenant, rootID, childBID, "trusted_telemetry_batch", "processed", 1, base, []byte(`{"temperature":99.0}`))
	addReceipt(tenant, rootID, childID, "legacy_telemetry", "processed", 1, base, []byte(`{"temperature":88.0}`))
	addReceipt(tenant, rootID, childID, "trusted_telemetry_batch", "pending", 1, base, []byte(`{"temperature":77.0}`))
	addReceipt(otherTenant, foreignDevice, childID, "trusted_telemetry_batch", "processed", 9, base, []byte(`{"temperature":66.0}`))
	var explain []string
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SET LOCAL enable_seqscan=off").Error; err != nil {
			return err
		}
		return tx.Raw(`EXPLAIN WITH candidates AS (
 SELECT id,created_at FROM broker_ingress_receipts
 WHERE tenant_id=? AND stream='telemetry_sample' AND state='processed'
   AND metadata->>'source'='trusted_telemetry_batch'
   AND metadata->>'logical_device_id'=? AND created_at>=? AND created_at<?
 ORDER BY created_at,id LIMIT 201
) SELECT id FROM candidates ORDER BY created_at,id`, tenant, childID, start, end).Scan(&explain).Error
	}); err != nil {
		t.Fatalf("explain bounded telemetry lookup: %v", err)
	}
	plan := strings.Join(explain, "\n")
	if !strings.Contains(plan, "broker_ingress_receipts_trusted_telemetry_device_time_idx") {
		t.Fatalf("bounded telemetry lookup did not use migration index: %s", plan)
	}
	var receiptCountBefore, activityCountBefore int64
	_ = db.Table("broker_ingress_receipts").Count(&receiptCountBefore).Error
	_ = db.Table("broker_telemetry_activity").Count(&activityCountBefore).Error

	gin.SetMode(gin.TestMode)
	call := func(claims *utils.UserClaims, headerToken, id string, query url.Values) *httptest.ResponseRecorder {
		t.Helper()
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		path := "/api/v1/device/telemetry/evidence/" + id
		if encoded := query.Encode(); encoded != "" {
			path += "?" + encoded
		}
		ctx.Request = httptest.NewRequest(http.MethodGet, path, nil)
		ctx.Params = gin.Params{{Key: "id", Value: id}}
		if headerToken != "" {
			ctx.Request.Header.Set("x-token", headerToken)
		}
		if claims != nil {
			ctx.Set("claims", claims)
		}
		(&DeviceApi{}).BrokerTelemetryEvidence(ctx)
		return recorder
	}

	query := url.Values{"start": {start.Format(time.RFC3339)}, "end": {end.Format(time.RFC3339)}, "keys": {"temperature"}}
	claims := &utils.UserClaims{ID: userID, TenantID: tenant, Authority: "TENANT_ADMIN"}
	response := call(claims, token, childID, query)
	if response.Code != http.StatusOK {
		t.Fatalf("trusted child evidence status=%d body=%s", response.Code, response.Body.String())
	}
	var result struct {
		SemanticPins struct {
			SnapshotID        string `json:"snapshot_id"`
			PublishedRevision uint64 `json:"published_revision"`
		} `json:"semantic_pins"`
		State struct {
			Status string `json:"status"`
		} `json:"state"`
		WindowBasis string `json:"window_basis"`
		Items       []struct {
			Key                   string     `json:"key"`
			Value                 float64    `json:"value"`
			Kind                  string     `json:"kind"`
			Unit                  string     `json:"unit"`
			UnitKnown             bool       `json:"unit_known"`
			ReportedAt            *time.Time `json:"reported_at"`
			ReportedTimeAvailable bool       `json:"reported_time_available"`
			TimestampBasis        string     `json:"timestamp_basis"`
			ObservedAt            time.Time  `json:"observed_at"`
			ProcessedAt           time.Time  `json:"processed_at"`
			ReceiptID             string     `json:"receipt_id"`
			TransportDeviceID     string     `json:"transport_device_id"`
			BrokerCell            string     `json:"broker_cell"`
			RouteEpoch            uint64     `json:"route_epoch"`
			Quality               string     `json:"quality"`
			Source                string     `json:"source"`
			ReceivedAt            *time.Time `json:"received_at"`
		} `json:"items"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal("decode telemetry evidence")
	}
	if len(result.Items) != 1 {
		t.Fatalf("returned points=%d body=%s", len(result.Items), response.Body.String())
	}
	item := result.Items[0]
	if item.ReceiptID != wantedID || item.Value != 22.5 || item.Kind != "FLOAT" || item.Unit != "°C" || !item.UnitKnown || item.Source != "trusted_telemetry_batch" || item.Quality != "trusted_processed" || item.ReportedAt != nil || item.ReportedTimeAvailable || item.TimestampBasis != "broker_observed" || !item.ObservedAt.Equal(base) || !item.ProcessedAt.Equal(base.Add(time.Second)) || item.ReceivedAt != nil || item.TransportDeviceID != rootID || item.BrokerCell != "telemetry-cell-old" || item.RouteEpoch != 1 {
		t.Fatalf("trusted observation provenance mismatch: %+v", item)
	}
	if result.SemanticPins.SnapshotID != "telemetry-snapshot" || result.SemanticPins.PublishedRevision != 7 || result.WindowBasis != "observed_at" {
		t.Fatalf("semantic/window metadata mismatch: %+v", result)
	}
	if semanticRequests.Load() != 4 {
		t.Fatalf("Encore HTTP requests=%d, want one Describe sequence (4 GETs)", semanticRequests.Load())
	}

	badWindow := url.Values{"start": {start.Add(-25 * time.Hour).Format(time.RFC3339)}, "end": {end.Format(time.RFC3339)}, "keys": {"temperature"}}
	if got := call(claims, token, childID, badWindow); got.Code != http.StatusBadRequest {
		t.Fatalf("overlong window status=%d", got.Code)
	}
	unsupportedKey := url.Values{"start": {start.Format(time.RFC3339)}, "end": {end.Format(time.RFC3339)}, "keys": {"label"}}
	if got := call(claims, token, childID, unsupportedKey); got.Code != http.StatusConflict {
		t.Fatalf("attribute key masquerading as telemetry status=%d", got.Code)
	}
	unitless := url.Values{"start": {start.Format(time.RFC3339)}, "end": {end.Format(time.RFC3339)}, "keys": {"online"}}
	unitlessResponse := call(claims, token, childID, unitless)
	var unitlessResult struct {
		Items []struct {
			Unit      string `json:"unit"`
			UnitKnown bool   `json:"unit_known"`
		} `json:"items"`
	}
	if unitlessResponse.Code != http.StatusOK || json.Unmarshal(unitlessResponse.Body.Bytes(), &unitlessResult) != nil || len(unitlessResult.Items) != 1 || unitlessResult.Items[0].UnitKnown || unitlessResult.Items[0].Unit != "" {
		t.Fatalf("missing semantic unit was guessed: status=%d body=%s", unitlessResponse.Code, unitlessResponse.Body.String())
	}
	missingMetric := url.Values{"start": {start.Format(time.RFC3339)}, "end": {end.Format(time.RFC3339)}, "keys": {"humidity"}}
	missingResponse := call(claims, token, childID, missingMetric)
	if missingResponse.Code != http.StatusOK || !strings.Contains(missingResponse.Body.String(), `"items":[]`) {
		t.Fatalf("missing metric value was filled or rejected: status=%d body=%s", missingResponse.Code, missingResponse.Body.String())
	}
	// The current writer's timestamp is broker-observed, so inconsistent metadata
	// is rejected instead of being exposed as a device-reported sample time.
	mismatchID := addReceipt(tenant, rootID, childID, "trusted_telemetry_batch", "processed", 1, base.Add(2*time.Minute), payload)
	if err := db.Exec(`UPDATE broker_ingress_receipts SET metadata=jsonb_set(metadata,'{timestamp}',to_jsonb(?::bigint),false) WHERE id=?`, base.Add(-5*time.Minute).UnixMilli(), mismatchID).Error; err != nil {
		t.Fatal("seed inconsistent timestamp contract")
	}
	_ = db.Table("broker_ingress_receipts").Count(&receiptCountBefore).Error
	_ = db.Table("broker_telemetry_activity").Count(&activityCountBefore).Error
	if got := call(claims, token, childID, query); got.Code != http.StatusServiceUnavailable {
		t.Fatalf("inconsistent trusted timestamp was presented as evidence: status=%d body=%s", got.Code, got.Body.String())
	}
	if got := call(claims, "", childID, query); got.Code != http.StatusUnauthorized {
		t.Fatalf("missing x-token status=%d", got.Code)
	}
	otherClaims := &utils.UserClaims{ID: userID, TenantID: otherTenant, Authority: "TENANT_ADMIN"}
	if got := call(otherClaims, token, childID, query); got.Code != http.StatusUnauthorized {
		t.Fatalf("cross-tenant token claims status=%d", got.Code)
	}
	if got := call(claims, token, foreignDevice, query); got.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant device status=%d", got.Code)
	}
	var receiptCountAfter, activityCountAfter int64
	_ = db.Table("broker_ingress_receipts").Count(&receiptCountAfter).Error
	_ = db.Table("broker_telemetry_activity").Count(&activityCountAfter).Error
	if receiptCountAfter != receiptCountBefore || activityCountAfter != activityCountBefore {
		t.Fatalf("read API mutated data: receipts %d->%d activity %d->%d", receiptCountBefore, receiptCountAfter, activityCountBefore, activityCountAfter)
	}
	var online int16
	if err := db.Table("devices").Select("is_online").Where("tenant_id=? AND id=?", tenant, childID).Scan(&online).Error; err != nil || online != 0 {
		t.Fatalf("read API modified legacy online projection: online=%d err=%v", online, err)
	}
}

func TestTelemetryEvidenceQueryRejectsRepeatedParameters(t *testing.T) {
	for _, query := range []string{
		"start=2026-10-20T00:00:00Z&start=2026-10-20T00:00:01Z&end=2026-10-20T01:00:00Z&keys=temp",
		"start=2026-10-20T00:00:00Z&end=2026-10-20T01:00:00Z&keys=temp&limit=10&limit=20",
		"start=2026-10-20T00:00:00Z&end=2026-10-20T01:00:00Z&keys=temp&unknown=x",
	} {
		if _, err := parseTelemetryEvidenceQuery(mustParseQuery(t, query)); err == nil {
			t.Fatalf("accepted ambiguous query %q", query)
		}
	}
}

func mustParseQuery(t *testing.T, raw string) url.Values {
	t.Helper()
	values, err := url.ParseQuery(raw)
	if err != nil {
		t.Fatal("parse query fixture")
	}
	return values
}

package brokerledger

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func postgresStore(t *testing.T, limits Limits) (*Store, *gorm.DB) {
	t.Helper()
	dsn := os.Getenv("TP_BROKER_LEDGER_TEST_DSN")
	if dsn == "" {
		dsnBytes, err := os.ReadFile("/tmp/tp-mqtt-20261019/qa-dsn")
		if err != nil {
			t.Skip("PostgreSQL QA DSN is unavailable")
		}
		dsn = strings.TrimSpace(string(dsnBytes))
	}
	if dsn == "" {
		t.Skip("PostgreSQL QA DSN is empty")
	}

	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: loggerForTest()})
	if err != nil {
		t.Fatalf("open PostgreSQL QA database: %v", err)
	}
	adminSQL, err := admin.DB()
	if err != nil {
		t.Fatal(err)
	}
	schema := "brokerledger_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quotedSchema := `"` + schema + `"` // schema derives only from hexadecimal UUID characters.
	if err := admin.Exec("CREATE SCHEMA " + quotedSchema).Error; err != nil {
		adminSQL.Close()
		t.Fatalf("create isolated PostgreSQL schema: %v", err)
	}
	var isolatedSQL *sql.DB
	t.Cleanup(func() {
		if isolatedSQL != nil {
			_ = isolatedSQL.Close()
		}
		_ = admin.Exec("DROP SCHEMA " + quotedSchema + " CASCADE").Error
		_ = adminSQL.Close()
	})
	isolatedDSN := dsnWithSearchPath(dsn, schema)
	db, err := gorm.Open(postgres.Open(isolatedDSN), &gorm.Config{Logger: loggerForTest()})
	if err != nil {
		_ = admin.Exec("DROP SCHEMA " + quotedSchema + " CASCADE").Error
		adminSQL.Close()
		t.Fatalf("open isolated PostgreSQL schema: %v", err)
	}
	if err := db.Exec(`CREATE TABLE broker_transport_bindings (
		tenant_id varchar(36) NOT NULL,
		device_id varchar(36) NOT NULL,
		broker_cell varchar(128) NOT NULL,
		route_epoch bigint NOT NULL DEFAULT 1 CHECK (route_epoch > 0),
		phase varchar(24) NOT NULL DEFAULT 'active' CHECK (phase IN ('active','connecting','draining')),
		updated_at timestamptz NOT NULL DEFAULT now(),
		PRIMARY KEY (tenant_id, device_id)
	)`).Error; err != nil {
		t.Fatalf("create isolated binding table: %v", err)
	}
	migration, err := os.ReadFile(filepath.Join("..", "..", "sql", "broker", "002_ledger.sql"))
	if err != nil {
		t.Fatalf("read ledger migration: %v", err)
	}
	if err := db.Exec(string(migration)).Error; err != nil {
		t.Fatalf("apply ledger migration: %v", err)
	}
	dbSQL, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	isolatedSQL = dbSQL
	dbSQL.SetMaxOpenConns(12)
	return NewWithLimits(db, limits), db
}

func loggerForTest() gormlogger.Interface {
	return gormlogger.Default.LogMode(gormlogger.Silent)
}

// dsnWithSearchPath accepts both PostgreSQL URLs and keyword/value DSNs.
func dsnWithSearchPath(dsn, schema string) string {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		parsed, err := url.Parse(dsn)
		if err == nil {
			query := parsed.Query()
			query.Set("search_path", schema)
			parsed.RawQuery = query.Encode()
			return parsed.String()
		}
	}
	return dsn + " search_path=" + schema
}

func insertBinding(t *testing.T, db *gorm.DB, tenant, device, cell string, epoch uint64, phase string) {
	t.Helper()
	err := db.Exec(`INSERT INTO broker_transport_bindings(tenant_id,device_id,broker_cell,route_epoch,phase)
		VALUES (?,?,?,?,?)`, tenant, device, cell, epoch, phase).Error
	if err != nil {
		t.Fatalf("insert transport binding: %v", err)
	}
}

func testMessage(tenant, device, source string) Message {
	return Message{
		TenantID: tenant, DeviceID: device, Stream: "events", SourceEventID: source,
		BrokerCell: "cell-a", RouteEpoch: 1, Payload: []byte(`{"temperature":22.5}`),
		Metadata: map[string]any{"topic": "devices/event/evt-1", "message_id": source},
	}
}

func TestPostgresConcurrentDuplicateAcceptUsesOneStableReceipt(t *testing.T) {
	store, db := postgresStore(t, Limits{})
	tenant, device := uuid.NewString(), uuid.NewString()
	insertBinding(t, db, tenant, device, "cell-a", 1, "active")

	const writers = 100
	ids := make(chan string, writers)
	dups := make(chan bool, writers)
	errs := make(chan error, writers)
	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			msg := testMessage(tenant, device, "event-100")
			msg.Metadata["connection_id"] = fmt.Sprintf("connection-%d", i)
			msg.Metadata["broker_instance"] = fmt.Sprintf("instance-%d", i)
			msg.Metadata["observed_at"] = time.Now().Add(time.Duration(i) * time.Second).UTC().Format(time.RFC3339Nano)
			id, duplicate, err := store.Accept(context.Background(), msg)
			if err != nil {
				errs <- err
				return
			}
			ids <- id
			dups <- duplicate
		}(i)
	}
	wg.Wait()
	close(ids)
	close(dups)
	close(errs)
	for err := range errs {
		t.Errorf("Accept: %v", err)
	}
	var onlyID string
	for id := range ids {
		if onlyID == "" {
			onlyID = id
		} else if id != onlyID {
			t.Fatalf("duplicate source event returned ids %q and %q", onlyID, id)
		}
	}
	var duplicateCount int
	for duplicate := range dups {
		if duplicate {
			duplicateCount++
		}
	}
	if duplicateCount != writers-1 {
		t.Fatalf("duplicate count = %d, want %d", duplicateCount, writers-1)
	}
	stats, err := store.Stats(context.Background(), tenant)
	if err != nil {
		t.Fatal(err)
	}
	if stats.PendingCount != 1 {
		t.Fatalf("pending count = %d, want 1", stats.PendingCount)
	}
	conflictingPayload := testMessage(tenant, device, "event-100")
	conflictingPayload.Payload = []byte(`{"temperature":23}`)
	if _, _, err := store.Accept(context.Background(), conflictingPayload); !errors.Is(err, ErrConflict) {
		t.Fatalf("different payload conflict = %v, want ErrConflict", err)
	}
	conflictingParams := testMessage(tenant, device, "event-100")
	conflictingParams.Metadata["schema_version"] = "2"
	if _, _, err := store.Accept(context.Background(), conflictingParams); !errors.Is(err, ErrConflict) {
		t.Fatalf("different stable metadata conflict = %v, want ErrConflict", err)
	}
}

func TestPostgresSameSourceEventWithDifferentIngressTimestampIsDuplicate(t *testing.T) {
	store, db := postgresStore(t, Limits{})
	tenant, device := uuid.NewString(), uuid.NewString()
	insertBinding(t, db, tenant, device, "cell-a", 1, "active")
	first := testMessage(tenant, device, "stable-source-event")
	first.Metadata["timestamp"] = int64(1000)
	id, duplicate, err := store.Accept(context.Background(), first)
	if err != nil || duplicate {
		t.Fatalf("first Accept id=%q duplicate=%v err=%v", id, duplicate, err)
	}
	second := first
	second.Metadata = map[string]any{"topic": "devices/event/evt-1", "message_id": "stable-source-event", "timestamp": int64(2000)}
	id2, duplicate, err := store.Accept(context.Background(), second)
	if err != nil || !duplicate || id2 != id {
		t.Fatalf("retry Accept id=%q duplicate=%v err=%v, want original receipt %q", id2, duplicate, err, id)
	}
}

func TestPostgresSameSamplesWithoutSourceIDRemainDistinct(t *testing.T) {
	store, db := postgresStore(t, Limits{})
	tenant, device := uuid.NewString(), uuid.NewString()
	insertBinding(t, db, tenant, device, "cell-a", 1, "active")
	seen := map[string]bool{}
	for i := 0; i < 10; i++ {
		msg := testMessage(tenant, device, "")
		id, duplicate, err := store.Accept(context.Background(), msg)
		if err != nil {
			t.Fatal(err)
		}
		if duplicate || seen[id] {
			t.Fatalf("sample %d unexpectedly deduplicated as %q", i, id)
		}
		seen[id] = true
	}
	stats, err := store.Stats(context.Background(), tenant)
	if err != nil {
		t.Fatal(err)
	}
	if stats.PendingCount != 10 {
		t.Fatalf("pending count = %d, want 10", stats.PendingCount)
	}
}

func TestPostgresRejectsWrongEpochAndScopesReceiptReads(t *testing.T) {
	store, db := postgresStore(t, Limits{})
	tenant, device := uuid.NewString(), uuid.NewString()
	insertBinding(t, db, tenant, device, "cell-a", 2, "active")
	wrong := testMessage(tenant, device, "wrong-epoch")
	if _, _, err := store.Accept(context.Background(), wrong); !errors.Is(err, ErrFenced) {
		t.Fatalf("wrong epoch error = %v, want ErrFenced", err)
	}
	valid := wrong
	valid.SourceEventID = "valid-epoch"
	valid.RouteEpoch = 2
	id, _, err := store.Accept(context.Background(), valid)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetReceipt(context.Background(), uuid.NewString(), device, id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-tenant receipt read = %v, want ErrNotFound", err)
	}
	if _, err := store.GetReceipt(context.Background(), tenant, uuid.NewString(), id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-device receipt read = %v, want ErrNotFound", err)
	}
	if err := db.Exec("UPDATE broker_transport_bindings SET route_epoch = 3 WHERE tenant_id = ? AND device_id = ?", tenant, device).Error; err != nil {
		t.Fatal(err)
	}
	called := false
	processed, err := store.ProcessOne(context.Background(), func(context.Context, *gorm.DB, Message) error {
		called = true
		return nil
	})
	if err != nil || !processed || called {
		t.Fatalf("fenced ProcessOne processed=%v called=%v err=%v", processed, called, err)
	}
	receipt, err := store.GetReceipt(context.Background(), tenant, device, id)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.State != stateFenced {
		t.Fatalf("receipt state = %q, want fenced", receipt.State)
	}
}

func TestPostgresHandlerRollbackRetryAndNoDuplicateProcessing(t *testing.T) {
	store, db := postgresStore(t, Limits{})
	tenant, device := uuid.NewString(), uuid.NewString()
	insertBinding(t, db, tenant, device, "cell-a", 1, "active")
	if err := db.Exec("CREATE TABLE handled_messages (receipt_id uuid PRIMARY KEY, payload bytea NOT NULL)").Error; err != nil {
		t.Fatal(err)
	}
	msg := testMessage(tenant, device, "event-rollback")
	id, _, err := store.Accept(context.Background(), msg)
	if err != nil {
		t.Fatal(err)
	}
	processed, err := store.ProcessOne(context.Background(), func(_ context.Context, tx *gorm.DB, m Message) error {
		if err := tx.Exec("INSERT INTO handled_messages(receipt_id,payload) VALUES (?,?)", m.ID, m.Payload).Error; err != nil {
			return err
		}
		return errors.New("simulated handler failure after business write")
	})
	if !processed || err == nil {
		t.Fatalf("failed handler processed=%v err=%v", processed, err)
	}
	var count int64
	if err := db.Table("handled_messages").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("business rows after rollback = %d, want 0", count)
	}
	if err := db.Exec("UPDATE broker_ingress_receipts SET retry_after = now() WHERE id = ?", id).Error; err != nil {
		t.Fatal(err)
	}
	processed, err = store.ProcessOne(context.Background(), func(_ context.Context, tx *gorm.DB, m Message) error {
		return tx.Exec("INSERT INTO handled_messages(receipt_id,payload) VALUES (?,?)", m.ID, m.Payload).Error
	})
	if err != nil || !processed {
		t.Fatalf("successful handler processed=%v err=%v", processed, err)
	}
	processed, err = store.ProcessOne(context.Background(), func(context.Context, *gorm.DB, Message) error {
		t.Fatal("processed receipt was delivered twice")
		return nil
	})
	if err != nil || processed {
		t.Fatalf("second ProcessOne processed=%v err=%v, want no work", processed, err)
	}
	if err := db.Table("handled_messages").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("business rows = %d, want 1", count)
	}
	receipt, err := store.GetReceipt(context.Background(), tenant, device, id)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.State != stateProcessed || receipt.Attempts != 1 {
		t.Fatalf("receipt state=%s attempts=%d, want processed and one failed attempt", receipt.State, receipt.Attempts)
	}
}

func TestPostgresHandlerFailuresBecomeAuditableDeadLetter(t *testing.T) {
	store, db := postgresStore(t, Limits{MaxAttempts: 2})
	tenant, device := uuid.NewString(), uuid.NewString()
	insertBinding(t, db, tenant, device, "cell-a", 1, "active")
	id, _, err := store.Accept(context.Background(), testMessage(tenant, device, "poison"))
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		processed, err := store.ProcessOne(context.Background(), func(context.Context, *gorm.DB, Message) error {
			return errors.New("poison payload")
		})
		if !processed || err == nil {
			t.Fatalf("attempt %d processed=%v err=%v", attempt+1, processed, err)
		}
		if attempt == 0 {
			if err := db.Exec("UPDATE broker_ingress_receipts SET retry_after = now() WHERE id = ?", id).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	receipt, err := store.GetReceipt(context.Background(), tenant, device, id)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.State != stateDead || receipt.Attempts != 2 || receipt.LastError != "poison payload" {
		t.Fatalf("dead receipt state=%s attempts=%d error=%q", receipt.State, receipt.Attempts, receipt.LastError)
	}
}

func TestPostgresRawPayloadLimitIsIndependentOfMetadataLimit(t *testing.T) {
	store, db := postgresStore(t, Limits{})
	tenant, device := uuid.NewString(), uuid.NewString()
	insertBinding(t, db, tenant, device, "cell-a", 1, "active")

	msg := testMessage(tenant, device, "raw-boundary")
	msg.Payload = make([]byte, defaultMessageLimit)
	msg.Metadata["diagnostic_context"] = strings.Repeat("m", 8<<10)
	if _, _, err := store.Accept(context.Background(), msg); err != nil {
		t.Fatalf("accept exactly 1 MiB raw payload with bounded metadata: %v", err)
	}

	tooMuchMetadata := testMessage(tenant, device, "metadata-over-limit")
	tooMuchMetadata.Metadata["diagnostic_context"] = strings.Repeat("m", 256<<10)
	if _, _, err := store.Accept(context.Background(), tooMuchMetadata); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("metadata over its independent limit error = %v, want ErrTooLarge", err)
	}
	stats, err := store.Stats(context.Background(), tenant)
	if err != nil {
		t.Fatal(err)
	}
	if stats.PendingCount != 1 {
		t.Fatalf("pending receipts after rejected oversized metadata = %d, want 1", stats.PendingCount)
	}
}

func TestPostgresCapacityAndOversizeRollBackAcceptance(t *testing.T) {
	store, db := postgresStore(t, Limits{MaxMessageBytes: 128, MaxPendingCount: 1, MaxPendingBytes: 1024})
	tenant, device := uuid.NewString(), uuid.NewString()
	insertBinding(t, db, tenant, device, "cell-a", 1, "active")
	large := testMessage(tenant, device, "large")
	large.Payload = make([]byte, 129)
	if _, _, err := store.Accept(context.Background(), large); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("oversized accept error = %v, want ErrTooLarge", err)
	}
	first := testMessage(tenant, device, "first")
	if _, _, err := store.Accept(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	second := testMessage(tenant, device, "second")
	if _, _, err := store.Accept(context.Background(), second); !errors.Is(err, ErrTenantCapacity) {
		t.Fatalf("capacity error = %v, want ErrTenantCapacity", err)
	}
	stats, err := store.Stats(context.Background(), tenant)
	if err != nil {
		t.Fatal(err)
	}
	if stats.PendingCount != 1 {
		t.Fatalf("pending count after rejected acceptance = %d, want 1", stats.PendingCount)
	}
}

func TestPostgresInactiveBindingRejectsAcceptance(t *testing.T) {
	store, db := postgresStore(t, Limits{})
	tenant, device := uuid.NewString(), uuid.NewString()
	insertBinding(t, db, tenant, device, "cell-a", 1, "draining")
	if _, _, err := store.Accept(context.Background(), testMessage(tenant, device, "draining")); !errors.Is(err, ErrBindingInactive) {
		t.Fatalf("inactive binding error = %v, want ErrBindingInactive", err)
	}
	stats, err := store.Stats(context.Background(), tenant)
	if err != nil {
		t.Fatal(err)
	}
	if stats.PendingCount != 0 {
		t.Fatalf("inactive binding created %d pending receipts", stats.PendingCount)
	}
}

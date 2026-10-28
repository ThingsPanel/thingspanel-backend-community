package brokerledger

import (
	"context"
	"errors"
	"fmt"
	"gorm.io/gorm"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func quarantineStore(t *testing.T) (*Store, *gorm.DB) {
	t.Helper()
	store, db := postgresStore(t, Limits{})
	migration, err := os.ReadFile(filepath.Join("..", "..", "sql", "broker", "008_quarantine.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(strings.ReplaceAll(string(migration), "public.", "")).Error; err != nil {
		t.Fatal(err)
	}
	return store, db
}

func rejection(tenant, device, topic string, body []byte) Rejection {
	return Rejection{TenantID: tenant, DeviceID: device, BrokerCell: "cell-a", RouteEpoch: 1, Topic: topic, Body: body, Reason: "invalid_business_payload"}
}

func insertQuarantineBinding(t *testing.T, db *gorm.DB, tenant, device string, epoch uint64, phase string) {
	t.Helper()
	insertBinding(t, db, tenant, device, "cell-a", epoch, phase)
}

func quarantineUsage(t *testing.T, db *gorm.DB, tenant string) (count, bytes int64) {
	t.Helper()
	var usage struct {
		Count int64 `gorm:"column:count"`
		Bytes int64 `gorm:"column:bytes"`
	}
	if err := db.Raw(`SELECT count(*) AS count, coalesce(sum(octet_length(body)),0) AS bytes FROM broker_ingress_quarantine WHERE tenant_id=?`, tenant).Scan(&usage).Error; err != nil {
		t.Fatal(err)
	}
	return usage.Count, usage.Bytes
}

func TestPostgresQuarantineConcurrentDuplicatesShareStableRecord(t *testing.T) {
	store, db := quarantineStore(t)
	tenant, device := "tenant-a", "device-a"
	insertQuarantineBinding(t, db, tenant, device, 1, "active")
	r := rejection(tenant, device, "devices/event/event-1", []byte{0, 1, 0xff})
	const writers = 64
	var wg sync.WaitGroup
	ids := make(chan string, writers)
	duplicates := make(chan bool, writers)
	errs := make(chan error, writers)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, duplicate, err := store.RecordRejection(context.Background(), r)
			if err != nil {
				errs <- err
				return
			}
			ids <- id
			duplicates <- duplicate
		}()
	}
	wg.Wait()
	close(ids)
	close(duplicates)
	close(errs)
	for err := range errs {
		t.Errorf("RecordRejection: %v", err)
	}
	var only string
	for id := range ids {
		if only == "" {
			only = id
		} else if only != id {
			t.Fatalf("unstable IDs %q != %q", id, only)
		}
	}
	var dupCount int
	for duplicate := range duplicates {
		if duplicate {
			dupCount++
		}
	}
	if dupCount != writers-1 {
		t.Fatalf("duplicates=%d want %d", dupCount, writers-1)
	}
	count, bytes := quarantineUsage(t, db, tenant)
	if count != 1 || bytes != int64(len(r.Body)) {
		t.Fatalf("usage count=%d bytes=%d", count, bytes)
	}
	var row quarantineRow
	if err := db.Where("id = ?", only).First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.Occurrences != writers || !row.ExpiresAt.After(time.Now().Add(6*24*time.Hour)) {
		t.Fatalf("unexpected quarantine row: %+v", row)
	}
}

func TestPostgresQuarantineTenantAndDeviceScopesAreIsolated(t *testing.T) {
	store, db := quarantineStore(t)
	insertQuarantineBinding(t, db, "ta", "da", 1, "active")
	insertQuarantineBinding(t, db, "ta", "db", 1, "active")
	insertQuarantineBinding(t, db, "tb", "da", 1, "active")
	inputs := []Rejection{
		rejection("ta", "da", "devices/event/x", []byte("bad")),
		rejection("ta", "db", "devices/event/x", []byte("bad")),
		rejection("tb", "da", "devices/event/x", []byte("bad")),
	}
	ids := map[string]bool{}
	for _, input := range inputs {
		id, duplicate, err := store.RecordRejection(context.Background(), input)
		if err != nil || duplicate {
			t.Fatalf("write %+v: id=%s dup=%v err=%v", input, id, duplicate, err)
		}
		if ids[id] {
			t.Fatalf("scope collision on id %s", id)
		}
		ids[id] = true
	}
	if count, _ := quarantineUsage(t, db, "ta"); count != 2 {
		t.Fatalf("tenant ta count=%d", count)
	}
	if count, _ := quarantineUsage(t, db, "tb"); count != 1 {
		t.Fatalf("tenant tb count=%d", count)
	}
}

func TestPostgresQuarantineRejectsFencedAndInactiveBindings(t *testing.T) {
	store, db := quarantineStore(t)
	insertQuarantineBinding(t, db, "ta", "da", 2, "active")
	old := rejection("ta", "da", "devices/event/x", []byte("bad"))
	old.RouteEpoch = 1
	if _, _, err := store.RecordRejection(context.Background(), old); !errors.Is(err, ErrQuarantineFenced) {
		t.Fatalf("old epoch err=%v", err)
	}
	old.RouteEpoch = 2
	if err := db.Exec(`UPDATE broker_transport_bindings SET phase='draining' WHERE tenant_id='ta' AND device_id='da'`).Error; err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.RecordRejection(context.Background(), old); !errors.Is(err, ErrQuarantineFenced) {
		t.Fatalf("draining binding err=%v", err)
	}
	if count, _ := quarantineUsage(t, db, "ta"); count != 0 {
		t.Fatalf("fenced rows written: %d", count)
	}
}

func TestPostgresQuarantineCapacityRollsBackAndDuplicateBypassesNewSlot(t *testing.T) {
	store, db := quarantineStore(t)
	insertQuarantineBinding(t, db, "ta", "da", 1, "active")
	insertQuarantineBinding(t, db, "ta", "db", 1, "active")
	store.quarantineCountLimit = 1
	store.quarantineBodyBytesLimit = 8
	first := rejection("ta", "da", "devices/event/one", []byte("123456"))
	if _, dup, err := store.RecordRejection(context.Background(), first); err != nil || dup {
		t.Fatalf("first: dup=%v err=%v", dup, err)
	}
	if _, dup, err := store.RecordRejection(context.Background(), first); err != nil || !dup {
		t.Fatalf("duplicate should update in-place: dup=%v err=%v", dup, err)
	}
	second := rejection("ta", "db", "devices/event/two", []byte("1"))
	if _, _, err := store.RecordRejection(context.Background(), second); !errors.Is(err, ErrQuarantineCapacity) {
		t.Fatalf("count capacity err=%v", err)
	}
	var count int64
	if err := db.Table("broker_ingress_quarantine").Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("capacity rollback count=%d err=%v", count, err)
	}
	store.quarantineCountLimit = 10
	store.quarantineBodyBytesLimit = 6
	if _, _, err := store.RecordRejection(context.Background(), second); !errors.Is(err, ErrQuarantineCapacity) {
		t.Fatalf("byte capacity err=%v", err)
	}
	if err := db.Table("broker_ingress_quarantine").Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("byte rollback count=%d err=%v", count, err)
	}
}

func TestPostgresQuarantineQuotaSerializesDifferentDevicesInTenant(t *testing.T) {
	store, db := quarantineStore(t)
	insertQuarantineBinding(t, db, "ta", "da", 1, "active")
	insertQuarantineBinding(t, db, "ta", "db", 1, "active")
	store.quarantineCountLimit = 1
	inputs := []Rejection{rejection("ta", "da", "devices/event/one", []byte("a")), rejection("ta", "db", "devices/event/two", []byte("b"))}
	var wg sync.WaitGroup
	errCh := make(chan error, len(inputs))
	for _, input := range inputs {
		wg.Add(1)
		go func(r Rejection) {
			defer wg.Done()
			_, _, err := store.RecordRejection(context.Background(), r)
			errCh <- err
		}(input)
	}
	wg.Wait()
	close(errCh)
	var accepted, capacity int
	for err := range errCh {
		if err == nil {
			accepted++
		} else if errors.Is(err, ErrQuarantineCapacity) {
			capacity++
		} else {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if accepted != 1 || capacity != 1 {
		t.Fatalf("accepted=%d capacity=%d", accepted, capacity)
	}
	if count, _ := quarantineUsage(t, db, "ta"); count != 1 {
		t.Fatalf("tenant quota oversubscribed: %d", count)
	}
}

func TestPostgresQuarantineDatabaseFailureRollsBack(t *testing.T) {
	store, db := quarantineStore(t)
	insertQuarantineBinding(t, db, "ta", "da", 1, "active")
	if err := db.Exec(`CREATE FUNCTION fail_quarantine_insert() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected failure'; END $$; CREATE TRIGGER fail_quarantine_insert BEFORE INSERT ON broker_ingress_quarantine FOR EACH ROW EXECUTE FUNCTION fail_quarantine_insert()`).Error; err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.RecordRejection(context.Background(), rejection("ta", "da", "devices/event/x", []byte("opaque"))); err == nil {
		t.Fatal("expected injected database failure")
	}
	if count, _ := quarantineUsage(t, db, "ta"); count != 0 {
		t.Fatalf("failed write left row: %d", count)
	}
}

func TestQuarantineRejectsInvalidTopicReasonAndOversizeBody(t *testing.T) {
	for _, r := range []Rejection{
		{TenantID: "t", DeviceID: "d", BrokerCell: "c", RouteEpoch: 1, Topic: "", Reason: "bad"},
		{TenantID: "t", DeviceID: "d", BrokerCell: "c", RouteEpoch: 1, Topic: "devices/#", Reason: "bad"},
		{TenantID: "t", DeviceID: "d", BrokerCell: "c", RouteEpoch: 1, Topic: "devices/\x01", Reason: "bad"},
		{TenantID: "t", DeviceID: "d", BrokerCell: "c", RouteEpoch: 1, Topic: "devices/event/x", Reason: strings.Repeat("x", QuarantineReasonLimit+1)},
		{TenantID: "t", DeviceID: "d", BrokerCell: "c", RouteEpoch: 1, Topic: "devices/event/x", Body: make([]byte, QuarantineBodyLimit+1), Reason: "bad"},
	} {
		if err := validateRejection(r); !errors.Is(err, ErrQuarantineInvalid) {
			t.Fatalf("invalid rejection accepted: %v", err)
		}
	}
}

func TestQuarantineIDIncludesEveryField(t *testing.T) {
	base := rejection("t", "d", "devices/event/x", []byte("body"))
	baseID := quarantineID(base)
	cases := []Rejection{base, base, base, base, base, base, base}
	cases[1].TenantID = "t2"
	cases[2].DeviceID = "d2"
	cases[3].BrokerCell = "c2"
	cases[4].RouteEpoch = 2
	cases[5].Topic = "devices/event/y"
	cases[6].Reason = "other"
	for i := 1; i < len(cases); i++ {
		if quarantineID(cases[i]) == baseID {
			t.Fatalf("field %d missing from ID", i)
		}
	}
	bodyChanged := base
	bodyChanged.Body = []byte(fmt.Sprint("body", "!"))
	if quarantineID(bodyChanged) == baseID {
		t.Fatal("body missing from ID")
	}
}

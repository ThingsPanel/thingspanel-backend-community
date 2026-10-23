package notification

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestConcurrentDifferentKeyReceiptRotationsHaveOneCurrentCredential(t *testing.T) {
	store, db := newMigratedTestStore(t)
	ctx := context.Background()
	instance := createFixtureInstance(t, store, "tenant-concurrent-rotation")
	// Widen the first-rotation race: without an instance-wide advisory lock,
	// competing transactions all observe no prior credential before inserting.
	if _, err := db.ExecContext(ctx, `CREATE FUNCTION slow_fixture_credential_insert() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_sleep(0.05); RETURN NEW; END $$; CREATE TRIGGER slow_fixture_credential BEFORE INSERT ON notification_credentials FOR EACH ROW EXECUTE FUNCTION slow_fixture_credential_insert()`); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 8)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			record := NotificationCredentialRecord{ID: fmt.Sprintf("rotation-%d", i), Purpose: CredentialPurposeReceipt, TokenHash: fmt.Sprintf("%064x", i+1), TenantID: "tenant-concurrent-rotation", InstanceID: instance, ExpiresAt: time.Now().UTC().Add(time.Hour)}
			_, _, err := store.RotateReceiptCredential(ctx, record.TenantID, fmt.Sprintf("rotation-key-%d", i), strings.Repeat("a", 64), "", record, []byte(`{"credential":"fixture-only-token"}`))
			results <- err
		}(i)
	}
	close(start)
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	var current, total int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FILTER (WHERE expires_at>$3),count(*) FROM notification_credentials WHERE tenant_id=$1 AND instance_id=$2 AND purpose='receipt' AND revoked_at IS NULL`, "tenant-concurrent-rotation", instance, time.Now().UTC().Add(16*time.Minute)).Scan(&current, &total); err != nil {
		t.Fatal(err)
	}
	if current != 1 || total != 8 {
		t.Fatalf("concurrent rotations left multiple unbounded old tokens: current=%d total=%d", current, total)
	}
}

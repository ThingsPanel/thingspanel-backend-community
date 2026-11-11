package brokercommands

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestPostgresCommandIdentityLifecycleRejectsBeforePersistence(t *testing.T) {
	for _, subject := range []string{"logical", "root"} {
		for _, mutation := range []string{"missing", "foreign", "tombstoned"} {
			t.Run(subject+"/"+mutation, func(t *testing.T) {
				store, db := postgresStore(t)
				tenant, root, target := uuid.NewString(), uuid.NewString(), uuid.NewString()
				seedDevices(t, db, tenant, root, target)
				installAuthoritativeIdentityFence(t, db)
				id := target
				if subject == "root" {
					id = root
				}
				var err error
				switch mutation {
				case "missing":
					err = db.Exec("DELETE FROM device_id_reservations WHERE id=?", id).Error
				case "foreign":
					err = db.Exec("UPDATE device_id_reservations SET tenant_id=? WHERE id=?", uuid.NewString(), id).Error
				case "tombstoned":
					err = db.Exec("UPDATE device_id_reservations SET state='tombstoned',deleted_at=now() WHERE id=?", id).Error
				}
				if err != nil {
					t.Fatal(err)
				}
				calls := 0
				_, _, err = store.Submit(context.Background(), newRequest(tenant, "caller", root, target, "identity"), func(tx *gorm.DB, row Row) error { calls++; return persistLog(tx, row) })
				if !errors.Is(err, ErrRejected) || calls != 0 {
					t.Fatalf("err=%v persistence calls=%d", err, calls)
				}
				var count int64
				if err := db.Model(&Row{}).Count(&count).Error; err != nil || count != 0 {
					t.Fatalf("accepted=%d err=%v", count, err)
				}
			})
		}
	}
}

func TestPostgresCommandIdentitySchemaIsMandatory(t *testing.T) {
	store, db := postgresStore(t)
	tenant, root, target := uuid.NewString(), uuid.NewString(), uuid.NewString()
	seedDevices(t, db, tenant, root, target)
	if err := db.Exec("DROP TABLE device_id_reservations").Error; err != nil {
		t.Fatal(err)
	}
	_, _, err := store.Submit(context.Background(), newRequest(tenant, "caller", root, target, "missing-schema"), persistLog)
	if !errors.Is(err, ErrIdentitySchema) {
		t.Fatalf("err=%v", err)
	}
}

func TestPostgresCommandIdentityLocksThroughSubmitAndDispatch(t *testing.T) {
	store, db := postgresStore(t)
	tenant, root, target := uuid.NewString(), uuid.NewString(), uuid.NewString()
	seedDevices(t, db, tenant, root, target)
	installAuthoritativeIdentityFence(t, db)
	assertLocked := func(id string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
		defer cancel()
		err := db.WithContext(ctx).Exec("UPDATE device_id_reservations SET state='tombstoned',deleted_at=now() WHERE id=?", id).Error
		if err == nil || ctx.Err() == nil {
			t.Fatalf("reservation mutation escaped owner transaction: %v", err)
		}
	}
	row, _, err := store.Submit(context.Background(), newRequest(tenant, "caller", root, target, "identity-lock"), func(tx *gorm.DB, row Row) error {
		assertLocked(target)
		assertLocked(root)
		return persistLog(tx, row)
	})
	if err != nil {
		t.Fatal(err)
	}
	guard := func(context.Context, *gorm.DB, Row) error { return nil }
	claim, err := store.Claim(context.Background(), guard)
	if err != nil || claim == nil || claim.ID != row.ID {
		t.Fatalf("claim=%v err=%v", claim, err)
	}
	published := false
	err = store.Dispatch(context.Background(), row.ID, guard, func(context.Context, Row) error {
		assertLocked(target)
		assertLocked(root)
		published = true
		return nil
	})
	if err != nil || !published {
		t.Fatalf("published=%v err=%v", published, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := db.WithContext(ctx).Exec("UPDATE device_id_reservations SET state='tombstoned',deleted_at=now() WHERE id=?", target).Error; err != nil {
		t.Fatalf("reservation lock survived commit: %v", err)
	}
}

func TestPostgresCommandDispatchFencesRetiredIdentity(t *testing.T) {
	store, db := postgresStore(t)
	tenant, root, target := uuid.NewString(), uuid.NewString(), uuid.NewString()
	seedDevices(t, db, tenant, root, target)
	installAuthoritativeIdentityFence(t, db)
	row, _, err := store.Submit(context.Background(), newRequest(tenant, "caller", root, target, "retired-before-dispatch"), persistLog)
	if err != nil {
		t.Fatal(err)
	}
	guard := func(context.Context, *gorm.DB, Row) error { return nil }
	if claim, err := store.Claim(context.Background(), guard); err != nil || claim == nil {
		t.Fatalf("claim=%v err=%v", claim, err)
	}
	if err := db.Exec("UPDATE device_id_reservations SET state='tombstoned',deleted_at=now() WHERE id=?", target).Error; err != nil {
		t.Fatal(err)
	}
	published := false
	if err := store.Dispatch(context.Background(), row.ID, guard, func(context.Context, Row) error { published = true; return nil }); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetScoped(context.Background(), tenant, row.ID)
	if err != nil || got.State != StateFenced || published {
		t.Fatalf("state=%s published=%v err=%v", got.State, published, err)
	}
	// Historical idempotent replay remains readable, without authorizing a new action.
	replay, duplicate, err := store.Submit(context.Background(), newRequest(tenant, "caller", root, target, "retired-before-dispatch"), persistLog)
	if err != nil || !duplicate || replay.ID != row.ID {
		t.Fatalf("historical replay=%v duplicate=%v err=%v", replay, duplicate, err)
	}
}

func TestPostgresCommandIdentityChecksIntermediateAncestor(t *testing.T) {
	store, db := postgresStore(t)
	tenant, root, target, middle := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	seedDevices(t, db, tenant, root, target)
	if err := db.Exec("INSERT INTO devices(id,tenant_id,is_enabled,parent_id) VALUES (?,?,'enabled',?)", middle, tenant, root).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("UPDATE devices SET parent_id=? WHERE id=?", middle, target).Error; err != nil {
		t.Fatal(err)
	}
	installAuthoritativeIdentityFence(t, db)
	if err := db.Exec("UPDATE device_id_reservations SET state='tombstoned',deleted_at=now() WHERE id=?", middle).Error; err != nil {
		t.Fatal(err)
	}
	_, _, err := store.Submit(context.Background(), newRequest(tenant, "caller", root, target, "middle-tombstone"), persistLog)
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("intermediate tombstone err=%v", err)
	}
}

func TestPostgresCommandIdentityCannotUseTemporaryShadow(t *testing.T) {
	_, db := postgresStore(t)
	tenant, root, target := uuid.NewString(), uuid.NewString(), uuid.NewString()
	seedDevices(t, db, tenant, root, target)
	installAuthoritativeIdentityFence(t, db)
	if err := db.Exec("UPDATE device_id_reservations SET state='tombstoned',deleted_at=now() WHERE id=?", target).Error; err != nil {
		t.Fatal(err)
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("CREATE TEMP TABLE device_id_reservations(id text,tenant_id text,state text,deleted_at timestamptz) ON COMMIT DROP").Error; err != nil {
			return err
		}
		if err := tx.Exec("INSERT INTO pg_temp.device_id_reservations VALUES (?,?,'active',NULL),(?,?,'active',NULL)", target, tenant, root, tenant).Error; err != nil {
			return err
		}
		return lockCommandIdentities(tx, tenant, target)
	})
	if !errors.Is(err, ErrRejected) {
		t.Fatalf("temporary shadow supplied identity authority: %v", err)
	}
}

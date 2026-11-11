package dal

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/ThingsPanel/thingspanel-backend-community/pkg/relational"
	"github.com/ThingsPanel/thingspanel-backend-community/pkg/relational/reltest"
	"project/internal/model"
	"project/internal/query"
	"project/pkg/global"
	"project/pkg/utils"
	componentsql "project/sql"
)

type insertIfAbsentProbe struct {
	ID    string `gorm:"column:id;primaryKey"`
	Other string `gorm:"column:other"`
}

var errA2Duplicate = errors.New("duplicate reservation")

func (insertIfAbsentProbe) TableName() string { return "insert_if_absent_probe" }

func containsA2(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestInsertIfAbsentPostgresMySQL(t *testing.T) {
	reltest.Run(t, "insert-if-absent", func(s *reltest.Scenario) {
		s.Step("create unique-key fixture", func(ctx context.Context, db *gorm.DB) (any, error) {
			return nil, db.WithContext(ctx).Exec(`CREATE TABLE insert_if_absent_probe (
				id VARCHAR(36) PRIMARY KEY, other VARCHAR(36) NOT NULL UNIQUE)`).Error
		})
		s.Step("new row", func(ctx context.Context, db *gorm.DB) (any, error) {
			return InsertIfAbsent(db.WithContext(ctx), &insertIfAbsentProbe{ID: "identity-1", Other: "other-1"}, "id")
		})
		s.Step("same primary key is idempotent", func(ctx context.Context, db *gorm.DB) (any, error) {
			return InsertIfAbsent(db.WithContext(ctx), &insertIfAbsentProbe{ID: "identity-1", Other: "other-2"}, "id")
		})
		s.Step("a different unique index is still an error", func(ctx context.Context, db *gorm.DB) (any, error) {
			return InsertIfAbsent(db.WithContext(ctx), &insertIfAbsentProbe{ID: "identity-2", Other: "other-1"}, "id")
		})
		s.Step("duplicate does not poison transaction and concurrent callers have one winner", func(ctx context.Context, db *gorm.DB) (any, error) {
			if err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				inserted, err := InsertIfAbsent(tx, &insertIfAbsentProbe{ID: "identity-1", Other: "other-3"}, "id")
				if err != nil || inserted {
					return fmt.Errorf("transaction duplicate inserted=%t err=%v, want false/nil", inserted, err)
				}
				inserted, err = InsertIfAbsent(tx, &insertIfAbsentProbe{ID: "identity-3", Other: "other-3"}, "id")
				if err != nil || !inserted {
					return fmt.Errorf("insert after duplicate inserted=%t err=%v, want true/nil", inserted, err)
				}
				return nil
			}); err != nil {
				return nil, err
			}
			var wg sync.WaitGroup
			results := make(chan error, 8)
			for i := 0; i < cap(results); i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					inserted, err := InsertIfAbsent(db.WithContext(ctx), &insertIfAbsentProbe{ID: "identity-race", Other: "other-race"}, "id")
					if err != nil {
						results <- err
					} else if inserted {
						results <- nil
					} else {
						results <- errA2Duplicate
					}
				}()
			}
			wg.Wait()
			close(results)
			winners, duplicates, failures := 0, 0, 0
			for err := range results {
				if err == nil {
					winners++
				} else if errors.Is(err, errA2Duplicate) {
					duplicates++
				} else {
					failures++
				}
			}
			if winners != 1 || duplicates != 7 || failures != 0 {
				return nil, fmt.Errorf("concurrent insert winners=%d duplicates=%d failures=%d, want 1/7/0", winners, duplicates, failures)
			}
			return fmt.Sprintf("winners=%d duplicates=%d failures=%d", winners, duplicates, failures), nil
		})
	})
}

func TestDeviceIDReservationTenantIsolationPostgresMySQL(t *testing.T) {
	reltest.Run(t, "device-id-reservation-tenant-boundary", func(s *reltest.Scenario) {
		s.Step("create isolated reservation table", func(ctx context.Context, db *gorm.DB) (any, error) {
			return nil, db.WithContext(ctx).Exec(`CREATE TABLE device_id_reservations (
				id VARCHAR(64) PRIMARY KEY, tenant_id VARCHAR(64) NOT NULL, state VARCHAR(16) NOT NULL,
				created_at TIMESTAMP NOT NULL, deleted_at TIMESTAMP NULL,
				CHECK ((state = 'active' AND deleted_at IS NULL) OR (state = 'tombstoned' AND deleted_at IS NOT NULL)))`).Error
		})
		s.Step("one tenant owns a device id globally", func(ctx context.Context, db *gorm.DB) (any, error) {
			firstErr := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				return reserveDeviceIDs(query.Use(tx), []*model.Device{{ID: "shared-device-id", TenantID: "tenant-a"}})
			})
			if firstErr != nil {
				return nil, firstErr
			}
			secondErr := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				return reserveDeviceIDs(query.Use(tx), []*model.Device{{ID: "shared-device-id", TenantID: "tenant-b"}})
			})
			if !errors.Is(secondErr, ErrDeviceIDReserved) {
				return nil, fmt.Errorf("cross-tenant reservation error = %v, want ErrDeviceIDReserved", secondErr)
			}
			var owner DeviceIDReservation
			if err := db.WithContext(ctx).Where("id = ?", "shared-device-id").Take(&owner).Error; err != nil {
				return nil, err
			}
			if owner.TenantID != "tenant-a" {
				return nil, fmt.Errorf("owner=%q, want tenant-a", owner.TenantID)
			}
			reserve := func(devices ...*model.Device) error {
				return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
					return reserveDeviceIDs(query.Use(tx), devices)
				})
			}
			if err := reserve(&model.Device{ID: "batch-new", TenantID: "tenant-a"}, &model.Device{ID: "shared-device-id", TenantID: "tenant-a"}); !errors.Is(err, ErrDeviceIDReserved) {
				return nil, fmt.Errorf("partially conflicting batch error=%v, want reserved", err)
			}
			var count int64
			if err := db.WithContext(ctx).Table("device_id_reservations").Where("id = ?", "batch-new").Count(&count).Error; err != nil {
				return nil, err
			}
			if count != 0 {
				return nil, fmt.Errorf("partially conflicting batch persisted %d rows, want 0", count)
			}
			for _, device := range []*model.Device{{ID: " ", TenantID: "tenant-a"}, {ID: "blank-tenant", TenantID: " "}} {
				if err := reserve(device); !errors.Is(err, ErrDeviceIdentityState) {
					return nil, fmt.Errorf("invalid reservation error=%v, want identity state", err)
				}
			}
			if err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return tombstoneDeviceID(query.Use(tx), "shared-device-id", "tenant-a") }); err != nil {
				return nil, fmt.Errorf("tombstone: %w", err)
			}
			for _, tc := range []struct{ id, tenant string }{{"shared-device-id", "tenant-a"}, {"shared-device-id", "tenant-a"}, {"shared-device-id", "tenant-b"}, {"missing-device", "tenant-a"}} {
				err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return tombstoneDeviceID(query.Use(tx), tc.id, tc.tenant) })
				if !errors.Is(err, ErrDeviceIdentityState) {
					return nil, fmt.Errorf("tombstone %s/%s error=%v, want identity state", tc.id, tc.tenant, err)
				}
			}
			if err := reserve(&model.Device{ID: "shared-device-id", TenantID: "tenant-a"}); !errors.Is(err, ErrDeviceIDReserved) {
				return nil, fmt.Errorf("re-reserve tombstoned id error=%v, want reserved", err)
			}
			if err := db.WithContext(ctx).Where("id = ?", "shared-device-id").Take(&owner).Error; err != nil {
				return nil, err
			}
			if owner.State != "tombstoned" || owner.DeletedAt == nil {
				return nil, fmt.Errorf("tombstone state=%s deleted_at=%v, want tombstoned/non-null", owner.State, owner.DeletedAt)
			}
			return "tenant isolation, batch rollback, validation, and tombstone lifecycle passed", nil
		})
	})
}

func TestDeviceTrendPostgresMySQL(t *testing.T) {
	reltest.Run(t, "device-trend-hourly-status", func(s *reltest.Scenario) {
		s.Step("create isolated trend tables", func(ctx context.Context, db *gorm.DB) (any, error) {
			idType, timeType := "BIGSERIAL", "TIMESTAMPTZ"
			if relational.MustDialectOf(db) == relational.MySQL {
				idType, timeType = "BIGINT AUTO_INCREMENT", "DATETIME(6)"
			}
			if err := db.WithContext(ctx).Exec(`CREATE TABLE devices (
				id VARCHAR(64) PRIMARY KEY, tenant_id VARCHAR(64) NOT NULL, created_at ` + timeType + ` NOT NULL)`).Error; err != nil {
				return nil, err
			}
			return nil, db.WithContext(ctx).Exec(`CREATE TABLE device_status_history (
				id ` + idType + ` PRIMARY KEY, tenant_id VARCHAR(64) NOT NULL, device_id VARCHAR(64) NOT NULL,
				status SMALLINT NOT NULL, change_time ` + timeType + ` NOT NULL)`).Error
		})
		s.Step("hourly trend preserves tenant scope and event semantics", func(ctx context.Context, db *gorm.DB) (any, error) {
			base := time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC)
			devices := []struct {
				id, tenant string
				created    time.Time
			}{{"a-online", "tenant-a", base.Add(-time.Hour)}, {"a-offline", "tenant-a", base.Add(-time.Hour)}, {"b-online", "tenant-b", base.Add(-time.Hour)}}
			for _, device := range devices {
				if err := db.WithContext(ctx).Exec(`INSERT INTO devices (id, tenant_id, created_at) VALUES (?, ?, ?)`, device.id, device.tenant, device.created).Error; err != nil {
					return nil, err
				}
			}
			events := []struct {
				device, tenant string
				status         int
				at             time.Time
			}{{"a-online", "tenant-a", 1, base.Add(-30 * time.Minute)}, {"a-online", "tenant-a", 0, base.Add(90 * time.Minute)},
				{"a-online", "tenant-a", 0, base.Add(120 * time.Minute)}, {"a-offline", "tenant-a", 1, base.Add(150 * time.Minute)},
				{"b-online", "tenant-b", 1, base.Add(30 * time.Minute)}}
			for _, event := range events {
				if err := db.WithContext(ctx).Exec(`INSERT INTO device_status_history (tenant_id, device_id, status, change_time) VALUES (?, ?, ?, ?)`, event.tenant, event.device, event.status, event.at).Error; err != nil {
					return nil, err
				}
			}
			previous := global.DB
			global.DB = db
			defer func() { global.DB = previous }()
			start, end := base.Unix(), base.Add(4*time.Hour).Unix()
			points, err := GetDeviceTrend("tenant-a", &start, &end)
			if err != nil {
				return nil, err
			}
			out := make([]string, 0, len(points))
			for _, point := range points {
				out = append(out, fmt.Sprintf("%d|%d|%d|%d", point.Timestamp.Unix(), point.DeviceTotal, point.DeviceOnline, point.DeviceOffline))
			}
			want := []string{
				fmt.Sprintf("%d|2|1|1", base.Unix()),
				fmt.Sprintf("%d|2|0|2", base.Add(time.Hour).Unix()),
				fmt.Sprintf("%d|2|1|1", base.Add(2*time.Hour).Unix()),
				fmt.Sprintf("%d|2|1|1", base.Add(3*time.Hour).Unix()),
				fmt.Sprintf("%d|2|1|1", base.Add(4*time.Hour).Unix()),
			}
			if !slices.Equal(out, want) {
				return nil, fmt.Errorf("trend points=%v, want %v", out, want)
			}
			return out, nil
		})
	})
}

func TestNotificationMemberContactTenantIsolationPostgresMySQL(t *testing.T) {
	reltest.Run(t, "notification-member-contact-tenant-boundary", func(s *reltest.Scenario) {
		s.Step("create isolated notification member fixture", func(ctx context.Context, db *gorm.DB) (any, error) {
			return nil, db.WithContext(ctx).Exec(`CREATE TABLE users (
				id VARCHAR(36) PRIMARY KEY, tenant_id VARCHAR(36) NOT NULL, email VARCHAR(255) NOT NULL,
				phone_number VARCHAR(64) NOT NULL, status VARCHAR(16) NOT NULL, authority VARCHAR(32) NOT NULL)`).Error
		})
		s.Step("trim contacts and reject cross-tenant lookup", func(ctx context.Context, db *gorm.DB) (any, error) {
			if err := db.WithContext(ctx).Exec(`INSERT INTO users (id, tenant_id, email, phone_number, status, authority)
				VALUES (?, ?, ?, ?, 'N', 'TENANT_USER')`, "member-a", "tenant-a", " member@example.test ", " 100-200 ").Error; err != nil {
				return nil, err
			}
			previous := global.DB
			global.DB = db
			defer func() { global.DB = previous }()
			contact, err := ResolveNotificationMemberContact(ctx, "member-a", "tenant-a", "email")
			if err != nil || contact.Address != "member@example.test" {
				return nil, fmt.Errorf("tenant contact=%q err=%v", contact.Address, err)
			}
			_, err = ResolveNotificationMemberContact(ctx, "member-a", "tenant-b", "email")
			if !errors.Is(err, ErrNotificationMemberNotFound) {
				return nil, fmt.Errorf("cross-tenant read err=%v, want not found", err)
			}
			return contact.TenantID + ":" + contact.Address + ":cross-tenant=not-found", nil
		})
	})
}

func TestTenantDefaultPolicyVersionPostgresMySQL(t *testing.T) {
	reltest.Run(t, "tenant-default-policy-version", func(s *reltest.Scenario) {
		s.Step("create isolated policy and lock fixtures", func(ctx context.Context, db *gorm.DB) (any, error) {
			if relational.MustDialectOf(db) == relational.MySQL {
				if err := db.WithContext(ctx).Exec(relational.AdvisoryLocksDDL).Error; err != nil {
					return nil, err
				}
			}
			return nil, db.WithContext(ctx).Exec(`CREATE TABLE notification_tenant_default_policies (
				source_deployment_id VARCHAR(128) NOT NULL, tenant_id VARCHAR(36) NOT NULL,
				native_group_id VARCHAR(128) NULL, alias_group_id VARCHAR(36) NULL,
				group_revision BIGINT NOT NULL, version BIGINT NOT NULL, updated_at TIMESTAMP NOT NULL,
				PRIMARY KEY (source_deployment_id, tenant_id))`).Error
		})
		s.Step("versions update atomically and remain tenant-scoped", func(ctx context.Context, db *gorm.DB) (any, error) {
			previous := global.DB
			global.DB = db
			defer func() { global.DB = previous }()
			first, err := SetTenantDefaultPolicy(ctx, "deployment-a2", "tenant-a", "", "", 0)
			if err != nil {
				return nil, err
			}
			second, err := SetTenantDefaultPolicy(ctx, "deployment-a2", "tenant-a", "", "", first.Version)
			if err != nil {
				return nil, err
			}
			other, err := SetTenantDefaultPolicy(ctx, "deployment-a2", "tenant-b", "", "", 0)
			if err != nil {
				return nil, err
			}
			if first.Version != 1 || second.Version != 2 || other.Version != 1 {
				return nil, fmt.Errorf("versions: tenant-a=%d,%d tenant-b=%d; want 1,2 and 1", first.Version, second.Version, other.Version)
			}
			var rows int64
			if err := db.WithContext(ctx).Table("notification_tenant_default_policies").Count(&rows).Error; err != nil {
				return nil, err
			}
			if rows != 2 {
				return nil, fmt.Errorf("policy rows=%d, want 2", rows)
			}
			return fmt.Sprintf("tenant-a=%d tenant-b=%d rows=%d", second.Version, other.Version, rows), nil
		})
	})
}

func TestUiMenuJSONArrayPostgresMySQL(t *testing.T) {
	reltest.Run(t, "ui-menu-json-array-membership", func(s *reltest.Scenario) {
		s.Step("create isolated UI menu schema", func(ctx context.Context, db *gorm.DB) (any, error) {
			authorityType := "JSONB"
			if relational.MustDialectOf(db) == relational.MySQL {
				authorityType = "JSON"
			}
			ddl := `CREATE TABLE sys_ui_elements (
				id VARCHAR(64) PRIMARY KEY, parent_id VARCHAR(64) NOT NULL, element_code VARCHAR(128) NOT NULL,
				element_type SMALLINT NOT NULL, orders SMALLINT NULL, param1 TEXT NULL, param2 TEXT NULL, param3 TEXT NULL,
				authority ` + authorityType + ` NOT NULL, description TEXT NULL, created_at TIMESTAMP NOT NULL,
				remark TEXT NULL, multilingual TEXT NULL, route_path TEXT NULL)`
			return nil, db.WithContext(ctx).Exec(ddl).Error
		})
		s.Step("role menus match array membership on both engines", func(ctx context.Context, db *gorm.DB) (any, error) {
			insert := `INSERT INTO sys_ui_elements (id,parent_id,element_code,element_type,orders,authority,created_at) VALUES (?,?,?,?,?,?,?)`
			if relational.MustDialectOf(db) == relational.Postgres {
				insert = `INSERT INTO sys_ui_elements (id,parent_id,element_code,element_type,orders,authority,created_at) VALUES (?,?,?,?,?,?::jsonb,?)`
			}
			base := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
			rows := [][]any{
				{"home-id", "0", "home", 1, 1, `["SYS_ADMIN","TENANT_ADMIN"]`, base},
				{"shared-id", "home-id", "shared", 3, 1, `["SYS_ADMIN","TENANT_ADMIN"]`, base},
				{"sys-id", "home-id", "sys-only", 3, 2, `["SYS_ADMIN"]`, base},
				{"tenant-id", "home-id", "tenant-only", 3, 3, `["TENANT_ADMIN"]`, base},
			}
			for _, row := range rows {
				if err := db.WithContext(ctx).Exec(insert, row...).Error; err != nil {
					return nil, err
				}
			}
			previous := global.DB
			global.DB = db
			query.SetDefault(db)
			defer func() { global.DB = previous }()
			var flatten func(nodes []*model.UiElementsListRsp, depth int, out *[]string)
			flatten = func(nodes []*model.UiElementsListRsp, depth int, out *[]string) {
				for _, node := range nodes {
					kind := "nil"
					if node.ElementType != nil {
						kind = fmt.Sprint(*node.ElementType)
					}
					*out = append(*out, fmt.Sprintf("%d:%s:%s", depth, node.ElementCode, kind))
					flatten(node.Children, depth+1, out)
				}
			}
			result := map[string][]string{}
			for _, authority := range []string{"SYS_ADMIN", "TENANT_ADMIN"} {
				_, data, err := ServeUiElementsListByAuthority(&utils.UserClaims{Authority: authority, ID: "user-a2"})
				if err != nil {
					return nil, err
				}
				list, ok := data.([]*model.UiElementsListRsp)
				if !ok {
					return nil, fmt.Errorf("menu response has type %T", data)
				}
				var flat []string
				flatten(list, 0, &flat)
				sort.Strings(flat)
				if len(flat) != 3 {
					return nil, fmt.Errorf("%s menu entries=%v, want root/shared/role-specific", authority, flat)
				}
				wanted := "1:sys-only:3"
				if authority == "TENANT_ADMIN" {
					wanted = "1:tenant-only:3"
				}
				if !containsA2(flat, "0:home:1") || !containsA2(flat, "1:shared:3") || !containsA2(flat, wanted) {
					return nil, fmt.Errorf("%s menu entries=%v, missing expected array membership %q", authority, flat, wanted)
				}
				result[authority] = flat
			}
			return result, nil
		})
	})
}

func TestServiceAccessOperationTenantIsolationPostgresMySQL(t *testing.T) {
	reltest.Run(t, "service-access-operation-tenant-boundary", func(s *reltest.Scenario) {
		s.Step("create isolated service access schema", func(ctx context.Context, db *gorm.DB) (any, error) {
			if relational.MustDialectOf(db) == relational.MySQL {
				for _, name := range componentsql.MySQLServiceAccessMigrationNames() {
					statements, err := componentsql.MySQLMigrationStatements(name)
					if err != nil {
						return nil, err
					}
					for _, statement := range statements {
						if err := db.WithContext(ctx).Exec(statement).Error; err != nil {
							return nil, err
						}
					}
				}
				return nil, nil
			}
			if err := db.WithContext(ctx).Exec(`CREATE TABLE service_access (
				id VARCHAR(36) PRIMARY KEY, name VARCHAR(100) NOT NULL, service_plugin_id VARCHAR(36) NOT NULL,
				voucher VARCHAR(999) NOT NULL, description VARCHAR(255), service_access_config JSON, remark VARCHAR(255),
				create_at TIMESTAMPTZ NOT NULL, update_at TIMESTAMPTZ NOT NULL, tenant_id VARCHAR(36) NOT NULL)`).Error; err != nil {
				return nil, err
			}
			migrations, err := componentsql.ServiceAccessMigrations()
			if err != nil {
				return nil, err
			}
			for _, migration := range migrations {
				if err := db.WithContext(ctx).Exec(string(migration.Contents)).Error; err != nil {
					return nil, err
				}
			}
			return nil, nil
		})
		s.Step("same operation key remains tenant-scoped", func(ctx context.Context, db *gorm.DB) (any, error) {
			query.SetDefault(db)
			base := time.Date(2026, 11, 4, 12, 0, 0, 123456000, time.UTC)
			requestHash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
			create := func(id, tenant, subject, key, hash string) (*model.ServiceAccessCreateObservation, error) {
				name, voucher := "access-"+tenant, "voucher-"+id
				config, description := `{"b": 1, "a": [1, 2]}`, "test"
				resource := &model.ServiceAccess{ID: id, Name: name, ServicePluginID: "plugin", Voucher: voucher,
					Description: &description, ServiceAccessConfig: &config, TenantID: tenant, CreateAt: base, UpdateAt: base}
				return CreateServiceAccessWithOperation(ctx, resource, subject, key, hash)
			}
			first, err := create("access-a", "tenant-a", "subject-a", "same-key", requestHash)
			if err != nil {
				return nil, err
			}
			second, err := create("access-b", "tenant-b", "subject-b", "same-key", requestHash)
			if err != nil {
				return nil, err
			}
			other, err := ReadServiceAccessCreateOperation(ctx, "tenant-c", "subject-a", "same-key")
			if err != nil {
				return nil, err
			}
			if first.ID != "access-a" || second.ID != "access-b" || other.State != "not_observed" {
				return nil, fmt.Errorf("tenant-scoped observations: tenant-a=%q tenant-b=%q other=%q", first.ID, second.ID, other.State)
			}
			replayed, err := create("access-a", "tenant-a", "subject-a", "same-key", requestHash)
			if err != nil || replayed == nil || !replayed.Replayed || replayed.State != "confirmed" {
				return nil, fmt.Errorf("same request replay=%v state=%q err=%v, want true/confirmed/nil", replayed, replayedState(replayed), err)
			}
			for _, tc := range []struct {
				name, subject, hash string
			}{
				{"different request hash", "subject-a", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
				{"different subject", "subject-other", requestHash},
			} {
				_, err := create("access-a", "tenant-a", tc.subject, "same-key", tc.hash)
				if !errors.Is(err, ErrServiceAccessOperationConflict) {
					return nil, fmt.Errorf("%s error=%v, want operation conflict", tc.name, err)
				}
			}
			read, err := ReadServiceAccessCreateOperation(ctx, "tenant-a", "subject-a", "same-key")
			if err != nil || read.State != "confirmed" || read.ID != "access-a" {
				return nil, fmt.Errorf("read confirmed receipt=%v err=%v", read, err)
			}
			_, err = ReadServiceAccessCreateOperation(ctx, "tenant-a", "subject-other", "same-key")
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, fmt.Errorf("read by another subject err=%v, want not found", err)
			}
			_, err = create("access-a", "tenant-a", "subject-a", "new-key", requestHash)
			if !relational.Is(err, relational.KindUnique) {
				return nil, fmt.Errorf("same resource under new key err=%v, want unique", err)
			}
			var receiptCount int64
			if err := db.WithContext(ctx).Model(&ServiceAccessCreateReceipt{}).Where("tenant_id = ?", "tenant-a").Count(&receiptCount).Error; err != nil {
				return nil, err
			}
			if receiptCount != 1 {
				return nil, fmt.Errorf("receipts after failed create=%d, want 1", receiptCount)
			}
			var wg sync.WaitGroup
			results := make(chan bool, 8)
			failures := make(chan error, 8)
			for i := 0; i < cap(results); i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					o, err := create("access-race", "tenant-race", "subject-race", "race-key", requestHash)
					if err != nil {
						failures <- err
						return
					}
					results <- o.Replayed
				}()
			}
			wg.Wait()
			close(results)
			close(failures)
			fresh, replayCount, failureCount := 0, 0, 0
			for replayed := range results {
				if replayed {
					replayCount++
				} else {
					fresh++
				}
			}
			for err := range failures {
				failureCount++
				_ = err
			}
			if fresh != 1 || replayCount != 7 || failureCount != 0 {
				return nil, fmt.Errorf("same-key concurrent create fresh=%d replay=%d errors=%d, want 1/7/0", fresh, replayCount, failureCount)
			}
			if err := db.WithContext(ctx).Exec("UPDATE service_access SET name = ? WHERE id = ? AND tenant_id = ?", "renamed", "access-a", "tenant-a").Error; err != nil {
				return nil, err
			}
			changed, err := ReadServiceAccessCreateOperation(ctx, "tenant-a", "subject-a", "same-key")
			if err != nil || changed.State != "requires_reconciliation" || changed.Reason != "RESOURCE_CHANGED" {
				return nil, fmt.Errorf("changed resource observation=%v err=%v", changed, err)
			}
			_, err = create("access-a", "tenant-a", "subject-a", "same-key", requestHash)
			if !errors.Is(err, ErrServiceAccessResourceChanged) {
				return nil, fmt.Errorf("replay after resource change err=%v, want resource changed", err)
			}
			if err := db.WithContext(ctx).Exec("DELETE FROM service_access WHERE id = ? AND tenant_id = ?", "access-a", "tenant-a").Error; err != nil {
				return nil, err
			}
			missing, err := ReadServiceAccessCreateOperation(ctx, "tenant-a", "subject-a", "same-key")
			if err != nil || missing.State != "requires_reconciliation" || missing.Reason != "RESOURCE_MISSING" {
				return nil, fmt.Errorf("deleted resource observation=%v err=%v", missing, err)
			}
			duplicateResourceErr := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				receipt := ServiceAccessCreateReceipt{TenantID: "tenant-c", OperationKey: "new-key", SubjectID: "subject-c",
					RequestHash: "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", AccessID: "access-a",
					ResourceHash: "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd", CreatedAt: base}
				_, err := InsertIfAbsent(tx, &receipt, "tenant_id", "operation_key")
				return err
			})
			if !relational.Is(duplicateResourceErr, relational.KindUnique) {
				return nil, fmt.Errorf("unrelated access_id unique conflict err=%v, want unique", duplicateResourceErr)
			}
			return fmt.Sprintf("tenant-scoped=%s/%s replay=true conflicts=2 concurrent=1/7 changed/deleted=requires_reconciliation unrelated-unique=preserved", first.ID, second.ID), nil
		})
	})
}

func replayedState(observation *model.ServiceAccessCreateObservation) string {
	if observation == nil {
		return "<nil>"
	}
	return observation.State
}

func TestSourceOutboxClaimTwoWorkersPostgresMySQL(t *testing.T) {
	reltest.Run(t, "source-outbox-claim-two-workers", func(s *reltest.Scenario) {
		s.Step("create isolated source outbox", func(ctx context.Context, db *gorm.DB) (any, error) {
			ddl := `CREATE TABLE notification_source_outbox (
				id VARCHAR(36) PRIMARY KEY, source_deployment_id VARCHAR(128) NOT NULL, tenant_id VARCHAR(36) NOT NULL,
				source_event_id VARCHAR(36) NOT NULL, source_action_id VARCHAR(36) NOT NULL,
				legacy_group_id VARCHAR(36) NOT NULL, notification_group_id VARCHAR(36) NOT NULL,
				group_revision BIGINT NOT NULL, idempotency_key VARCHAR(128) NOT NULL,
				request_body BYTEA NOT NULL, body_sha256 CHAR(64) NOT NULL,
				occurred_at TIMESTAMPTZ NOT NULL, expires_at TIMESTAMPTZ NOT NULL,
				state VARCHAR(24) NOT NULL, attempts INT NOT NULL DEFAULT 0,
				next_attempt_at TIMESTAMPTZ NOT NULL, lease_token VARCHAR(36) NULL, lease_until TIMESTAMPTZ NULL,
				handed_off_at TIMESTAMP NULL, failure_code VARCHAR(64) NULL,
				created_at TIMESTAMPTZ NOT NULL, updated_at TIMESTAMPTZ NOT NULL)`
			if relational.MustDialectOf(db) == relational.MySQL {
				ddl = `CREATE TABLE notification_source_outbox (
					id CHAR(36) PRIMARY KEY, source_deployment_id VARCHAR(128) NOT NULL, tenant_id VARCHAR(36) NOT NULL,
					source_event_id VARCHAR(36) NOT NULL, source_action_id VARCHAR(36) NOT NULL,
					legacy_group_id VARCHAR(36) NOT NULL, notification_group_id VARCHAR(36) NOT NULL,
					group_revision BIGINT NOT NULL, idempotency_key VARCHAR(128) NOT NULL,
					request_body LONGBLOB NOT NULL, body_sha256 CHAR(64) NOT NULL,
					occurred_at DATETIME(6) NOT NULL, expires_at DATETIME(6) NOT NULL,
					state VARCHAR(24) NOT NULL, attempts INT NOT NULL DEFAULT 0,
					next_attempt_at DATETIME(6) NOT NULL, lease_token CHAR(36) NULL, lease_until DATETIME(6) NULL,
					handed_off_at DATETIME(6) NULL, failure_code VARCHAR(64) NULL,
					created_at DATETIME(6) NOT NULL, updated_at DATETIME(6) NOT NULL,
					KEY idx_notification_source_outbox_claim (next_attempt_at, created_at)) ENGINE=InnoDB`
			}
			if err := db.WithContext(ctx).Exec(ddl).Error; err != nil {
				return nil, err
			}
			if relational.MustDialectOf(db) == relational.Postgres {
				return nil, db.WithContext(ctx).Exec(`CREATE INDEX idx_notification_source_outbox_claim ON notification_source_outbox (next_attempt_at, created_at)`).Error
			}
			return nil, nil
		})
		s.Step("four concurrent workers claim eight rows exactly once", func(ctx context.Context, db *gorm.DB) (any, error) {
			previous := global.DB
			global.DB = db
			defer func() { global.DB = previous }()
			base := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
			seed := func(name, state string, attempts int, next, leaseUntil time.Time) (string, error) {
				id := "eligibility-" + name
				var lease *time.Time
				var token *string
				tokenValue := "lease-" + name
				if state == "leased" {
					lease, token = &leaseUntil, &tokenValue
				}
				row := SourceOutboxRecord{ID: id, SourceDeploymentID: "dep-eligibility-a2", TenantID: "tenant-a2",
					SourceEventID: "event-" + id, SourceActionID: "action-" + id, LegacyGroupID: "group-a2",
					NotificationGroupID: "native-group-a2", GroupRevision: 1, IdempotencyKey: "key-" + id,
					RequestBody: []byte(`{}`), BodySHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
					OccurredAt: base.Add(-time.Hour), ExpiresAt: base.Add(time.Hour), State: state, Attempts: attempts,
					NextAttemptAt: next, LeaseToken: token, LeaseUntil: lease, CreatedAt: next, UpdatedAt: base}
				return id, db.WithContext(ctx).Create(&row).Error
			}
			past, expired, future := base.Add(-time.Minute), base.Add(-time.Second), base.Add(time.Minute)
			for _, item := range []struct {
				name     string
				state    string
				attempts int
				next     time.Time
				lease    time.Time
			}{{"due-1", "pending", 0, past.Add(-3 * time.Second), time.Time{}}, {"due-2", "retry_wait", 1, past.Add(-2 * time.Second), time.Time{}},
				{"due-3", "pending", 0, past.Add(-time.Second), time.Time{}}, {"future", "pending", 0, base.Add(time.Hour), time.Time{}},
				{"expired-lease", "leased", 1, past.Add(-4 * time.Second), expired}, {"active-lease", "leased", 1, past, future},
				{"exhausted", "leased", 5, past, expired}} {
				if _, err := seed(item.name, item.state, item.attempts, item.next, item.lease); err != nil {
					return nil, err
				}
			}
			first, err := ClaimSourceOutbox(ctx, 2, time.Minute, 5, base)
			if err != nil || len(first) != 2 || first[0].ID != "eligibility-expired-lease" || first[1].ID != "eligibility-due-1" {
				return nil, fmt.Errorf("ordered first claim=%v err=%v, want expired lease then earliest due", outboxIDs(first), err)
			}
			second, err := ClaimSourceOutbox(ctx, 10, time.Minute, 5, base)
			if err != nil || len(second) != 2 || second[0].ID != "eligibility-due-2" || second[1].ID != "eligibility-due-3" {
				return nil, fmt.Errorf("second eligible claim=%v err=%v, want retry_wait and due", outboxIDs(second), err)
			}
			third, err := ClaimSourceOutbox(ctx, 10, time.Minute, 5, base)
			if err != nil || len(third) != 0 {
				return nil, fmt.Errorf("ineligible rows were claimable: ids=%v err=%v", outboxIDs(third), err)
			}
			var exhausted SourceOutboxRecord
			if err := db.WithContext(ctx).Where("id = ?", "eligibility-exhausted").Take(&exhausted).Error; err != nil {
				return nil, err
			}
			if exhausted.State != "failed" || exhausted.FailureCode == nil || *exhausted.FailureCode != "retry_exhausted" {
				return nil, fmt.Errorf("exhausted row state=%s failure=%v, want failed/retry_exhausted", exhausted.State, exhausted.FailureCode)
			}
			for i := 0; i < 8; i++ {
				id := uuid.NewString()
				row := SourceOutboxRecord{ID: id, SourceDeploymentID: "dep-claim-a2", TenantID: "tenant-a2",
					SourceEventID: uuid.NewString(), SourceActionID: uuid.NewString(), LegacyGroupID: uuid.NewString(),
					NotificationGroupID: uuid.NewString(), GroupRevision: 1, IdempotencyKey: "key-" + id,
					RequestBody: []byte(`{}`), BodySHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
					OccurredAt: base.Add(-time.Hour), ExpiresAt: base.Add(time.Hour), State: "pending",
					NextAttemptAt: base.Add(-time.Minute), CreatedAt: base.Add(time.Duration(i) * time.Second), UpdatedAt: base}
				if err := db.WithContext(ctx).Create(&row).Error; err != nil {
					return nil, err
				}
			}
			var mu sync.Mutex
			var wg sync.WaitGroup
			seen := map[string]int{}
			errors := 0
			for worker := 0; worker < 4; worker++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					claimed, err := ClaimSourceOutbox(ctx, 2, time.Minute, 5, base)
					mu.Lock()
					defer mu.Unlock()
					if err != nil {
						errors++
						return
					}
					for _, row := range claimed {
						seen[row.ID]++
					}
				}()
			}
			wg.Wait()
			duplicates := 0
			for _, count := range seen {
				if count > 1 {
					duplicates++
				}
			}
			if len(seen) != 8 || duplicates != 0 || errors != 0 {
				return nil, fmt.Errorf("claims: got %d rows, %d duplicates, %d worker errors; want 8, 0, 0", len(seen), duplicates, errors)
			}
			var states struct {
				Total         int
				WrongState    int
				WrongAttempts int
			}
			if err := db.WithContext(ctx).Raw(`SELECT COUNT(*) AS total,
				SUM(CASE WHEN state = 'leased' THEN 0 ELSE 1 END) AS wrong_state,
				SUM(CASE WHEN attempts = 1 THEN 0 ELSE 1 END) AS wrong_attempts
				FROM notification_source_outbox WHERE source_deployment_id = ?`, "dep-claim-a2").Scan(&states).Error; err != nil {
				return nil, err
			}
			if states.Total != 8 || states.WrongState != 0 || states.WrongAttempts != 0 {
				return nil, fmt.Errorf("post-claim state: total=%d wrong_state=%d wrong_attempts=%d; want 8, 0, 0", states.Total, states.WrongState, states.WrongAttempts)
			}
			return fmt.Sprintf("claimed=%d duplicates=%d errors=%d total=%d wrong_state=%d wrong_attempts=%d", len(seen), duplicates, errors, states.Total, states.WrongState, states.WrongAttempts), nil
		})
	})
}

func outboxIDs(rows []SourceOutboxRecord) []string {
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	return ids
}

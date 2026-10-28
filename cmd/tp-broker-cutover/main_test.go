package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"project/internal/brokercontrol"
)

func TestCutoverPlanStrictAndExactDigest(t *testing.T) {
	valid := `{"version":"1","operation":"freeze","tenant_id":"tenant-a","device_id":"device-a","change_id":"` + uuid.NewString() + `","expected_cell":"cell-a","target_cell":"cell-b","expected_epoch":1,"members_sha256":"` + strings.Repeat("a", 64) + `"}`
	if _, err := parsePlan([]byte(valid)); err != nil {
		t.Fatalf("valid freeze plan rejected: %v", err)
	}
	for name, raw := range map[string]string{
		"duplicate key":            strings.Replace(valid, `"version":"1"`, `"version":"1","version":"1"`, 1),
		"unknown key":              strings.TrimSuffix(valid, "}") + `,"force":true}`,
		"irrelevant null field":    strings.TrimSuffix(valid, "}") + `,"target_ready_sha256":null}`,
		"tenant uuid not required": strings.Replace(valid, "tenant-a", "", 1),
		"floating epoch":           strings.Replace(valid, `"expected_epoch":1`, `"expected_epoch":1.0`, 1),
		"trailing JSON":            valid + ` {}`,
		"uppercase hash":           strings.Replace(valid, strings.Repeat("a", 64), strings.Repeat("A", 64), 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parsePlan([]byte(raw)); err == nil {
				t.Fatal("invalid plan accepted")
			}
		})
	}

	path := filepath.Join(t.TempDir(), "plan.json")
	if err := os.WriteFile(path, []byte(valid), 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(valid))
	var stdout, stderr strings.Builder
	code := run(context.Background(), []string{"apply", "--plan-file", path, "--sha256", strings.Repeat("0", 64), "--actor", "operator-a"}, func(string) string { return "" }, &stdout, &stderr)
	if code != 64 || !strings.Contains(stderr.String(), "does not match") || stdout.Len() != 0 {
		t.Fatalf("bad digest result code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if hex.EncodeToString(sum[:]) == strings.Repeat("0", 64) {
		t.Fatal("unexpected test digest collision")
	}
}

func TestCutoverDSNFileRequirements(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("0600 file mode verification requires Unix permissions; Windows callers can use the DSN environment variable")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "dsn")
	if err := os.WriteFile(path, []byte("postgres://safe"), 0640); err != nil {
		t.Fatal(err)
	}
	// Establish the insecure fixture explicitly: a restrictive process umask
	// may have reduced WriteFile's requested 0640 to the supported 0600 mode.
	if err := os.Chmod(path, 0640); err != nil {
		t.Fatal(err)
	}
	if _, err := readDSN(path, nil); err == nil {
		t.Fatal("permissive DSN file mode accepted")
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	if value, err := readDSN(path, nil); err != nil || value != "postgres://safe" {
		t.Fatalf("0600 DSN read value=%q err=%v", value, err)
	}
}

func TestCutoverErrorRedaction(t *testing.T) {
	secretDSN := "postgres://cutover-secret:do-not-print@127.0.0.1:1/brokerqa?connect_timeout=1"
	var stdout, stderr strings.Builder
	code := run(context.Background(), []string{"members", "--cell", "cell-a"}, func(k string) string {
		if k == "TP_BROKER_CUTOVER_DSN" {
			return secretDSN
		}
		return ""
	}, &stdout, &stderr)
	if code != 69 || strings.Contains(stderr.String(), secretDSN) || strings.Contains(stderr.String(), "do-not-print") || stdout.Len() != 0 {
		t.Fatalf("database error leaked configuration code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestCutoverPostgresFreezeRetryAndBarrierFailClosed(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("TEST_BROKER_DATABASE_DSN"))
	if dsn == "" {
		t.Skip("TEST_BROKER_DATABASE_DSN not supplied")
	}
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent), DisableAutomaticPing: true})
	if err != nil {
		t.Fatal("open QA broker database")
	}
	adminSQL, err := admin.DB()
	if err != nil {
		t.Fatal("obtain QA database pool")
	}
	adminSQL.SetMaxOpenConns(2)
	t.Cleanup(func() { _ = adminSQL.Close() })
	var database string
	if err := admin.Raw("SELECT current_database()").Scan(&database).Error; err != nil || database != "brokerqa" {
		t.Fatal("refusing test outside brokerqa")
	}
	schema := "brokerqa_cutover_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quoted := `"` + schema + `"`
	if err := admin.Exec("CREATE SCHEMA " + quoted).Error; err != nil {
		t.Fatal("create private schema")
	}
	t.Cleanup(func() { _ = admin.Exec("DROP SCHEMA " + quoted + " CASCADE").Error })
	testDSN := cutoverDSNWithSchema(dsn, schema)
	db, err := gorm.Open(postgres.Open(testDSN), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent), DisableAutomaticPing: true})
	if err != nil {
		t.Fatal("open private-schema database")
	}
	dbSQL, err := db.DB()
	if err != nil {
		t.Fatal("obtain private pool")
	}
	dbSQL.SetMaxOpenConns(2)
	t.Cleanup(func() { _ = dbSQL.Close() })
	paths, err := filepath.Glob(filepath.Join("..", "..", "sql", "broker", "[0-9][0-9][0-9]_*.sql"))
	if err != nil {
		t.Fatal("list migrations")
	}
	sort.Strings(paths)
	if len(paths) < 12 {
		t.Fatal("route migration unavailable")
	}
	for _, path := range paths {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal("read migration")
		}
		ddl := strings.ReplaceAll(string(body), "public.", quoted+".")
		if err := db.Exec(ddl).Error; err != nil {
			t.Fatalf("apply private migration %s: %v", filepath.Base(path), err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	store := brokercontrol.NewMigrationStore(db)
	for _, role := range []string{"broker", "adapter", "dispatcher", "effects"} {
		if _, err := store.RegisterMember(ctx, "cell-source", role, role+"-one", "boot-one"); err != nil {
			t.Fatalf("register source member %s: %v", role, err)
		}
	}
	if err := db.Exec(`INSERT INTO broker_transport_bindings(tenant_id,device_id,broker_cell,route_epoch,phase) VALUES('tenant-cutover','device-cutover','cell-source',1,'active')`).Error; err != nil {
		t.Fatal("seed isolated active binding")
	}
	_, digest, err := store.Members(ctx, "cell-source")
	if err != nil {
		t.Fatal("read fresh roster")
	}

	var membersOut, membersErr strings.Builder
	code := run(ctx, []string{"members", "--cell", "cell-source"}, func(k string) string {
		if k == "TP_BROKER_CUTOVER_DSN" {
			return testDSN
		}
		return ""
	}, &membersOut, &membersErr)
	if code != 0 {
		t.Fatalf("members command code=%d stderr=%s", code, membersErr.String())
	}
	var memberResult membersOutput
	if err := json.Unmarshal([]byte(membersOut.String()), &memberResult); err != nil || len(memberResult.Members) != 4 || memberResult.MembersSHA256 != digest {
		t.Fatalf("members output=%s err=%v", membersOut.String(), err)
	}

	changeID := uuid.NewString()
	rawPlan := []byte(fmt.Sprintf(`{"version":"1","operation":"freeze","tenant_id":"tenant-cutover","device_id":"device-cutover","change_id":%q,"expected_cell":"cell-source","target_cell":"cell-target","expected_epoch":1,"members_sha256":%q}`, changeID, digest))
	planPath := filepath.Join(t.TempDir(), "reviewed-plan.json")
	if err := os.WriteFile(planPath, rawPlan, 0600); err != nil {
		t.Fatal("write plan")
	}
	sum := sha256.Sum256(rawPlan)
	hash := hex.EncodeToString(sum[:])
	args := []string{"apply", "--plan-file", planPath, "--sha256", hash, "--actor", "operator-test"}
	getenv := func(k string) string {
		if k == "TP_BROKER_CUTOVER_DSN" {
			return testDSN
		}
		return ""
	}
	for i := 0; i < 2; i++ {
		var out, stderr strings.Builder
		if code := run(ctx, args, getenv, &out, &stderr); code != 0 {
			t.Fatalf("freeze apply #%d code=%d stderr=%s", i+1, code, stderr.String())
		}
		var got commandOutput
		if err := json.Unmarshal([]byte(out.String()), &got); err != nil || got.PlanHash != hash || got.Operation != "freeze" || got.Result != "applied" {
			t.Fatalf("freeze output=%s err=%v", out.String(), err)
		}
	}
	var changes, events, unacked int64
	if err := db.Table("broker_route_changes").Where("id=?", changeID).Count(&changes).Error; err != nil {
		t.Fatal("count changes")
	}
	if err := db.Table("broker_route_change_events").Where("change_id=?", changeID).Count(&events).Error; err != nil {
		t.Fatal("count events")
	}
	if err := db.Table("broker_route_barrier_members").Where("change_id=? AND acknowledged_at IS NULL", changeID).Count(&unacked).Error; err != nil {
		t.Fatal("count unacked members")
	}
	if changes != 1 || events != 1 || unacked != 4 {
		t.Fatalf("idempotent freeze changes=%d events=%d unacked=%d", changes, events, unacked)
	}

	readyHash := strings.Repeat("b", 64)
	prepareRaw := []byte(fmt.Sprintf(`{"version":"1","operation":"prepare","tenant_id":"tenant-cutover","device_id":"device-cutover","change_id":%q,"target_ready_sha256":%q}`, changeID, readyHash))
	preparePath := filepath.Join(t.TempDir(), "prepare.json")
	if err := os.WriteFile(preparePath, prepareRaw, 0600); err != nil {
		t.Fatal("write prepare plan")
	}
	prepareSum := sha256.Sum256(prepareRaw)
	var out, stderr strings.Builder
	prepareArgs := []string{"apply", "--plan-file", preparePath, "--sha256", hex.EncodeToString(prepareSum[:]), "--actor", "operator-test"}
	if code := run(ctx, prepareArgs, getenv, &out, &stderr); code != 1 || out.Len() != 0 {
		t.Fatalf("prepare without runtime ACK code=%d out=%q stderr=%q", code, out.String(), stderr.String())
	}
	if err := db.Table("broker_route_changes").Where("id=? AND phase='frozen'", changeID).Count(&changes).Error; err != nil || changes != 1 {
		t.Fatalf("prepare without ACK changed phase count=%d err=%v", changes, err)
	}

	wrongTenant := []byte(fmt.Sprintf(`{"version":"1","operation":"prepare","tenant_id":"tenant-other","device_id":"device-cutover","change_id":%q,"target_ready_sha256":%q}`, changeID, readyHash))
	wrongPath := filepath.Join(t.TempDir(), "wrong-tenant.json")
	if err := os.WriteFile(wrongPath, wrongTenant, 0600); err != nil {
		t.Fatal("write wrong-tenant plan")
	}
	wrongSum := sha256.Sum256(wrongTenant)
	var wrongOut, wrongErr strings.Builder
	wrongArgs := []string{"apply", "--plan-file", wrongPath, "--sha256", hex.EncodeToString(wrongSum[:]), "--actor", "operator-test"}
	if code := run(ctx, wrongArgs, getenv, &wrongOut, &wrongErr); code != 1 || wrongOut.Len() != 0 {
		t.Fatalf("wrong-tenant plan code=%d out=%q", code, wrongOut.String())
	}

	// This fixture ACKs the already registered test members through the store
	// API. It checks CLI audit propagation only; it is not runtime fence proof.
	for _, role := range []string{"broker", "adapter", "dispatcher", "effects"} {
		if _, err := store.RegisterMember(ctx, "cell-target", role, role+"-target", "boot-target"); err != nil {
			t.Fatalf("register target member %s: %v", role, err)
		}
		if err := store.AcknowledgeFence(ctx, "tenant-cutover", "device-cutover", changeID, role, role+"-one", "boot-one", strings.Repeat("c", 64)); err != nil {
			t.Fatalf("acknowledge fixture source member %s: %v", role, err)
		}
	}

	var prepareOut, prepareErr strings.Builder
	if code := run(ctx, prepareArgs, getenv, &prepareOut, &prepareErr); code != 0 {
		t.Fatalf("prepare apply code=%d stderr=%s", code, prepareErr.String())
	}
	var prepared commandOutput
	if err := json.Unmarshal([]byte(prepareOut.String()), &prepared); err != nil || prepared.Operation != "prepare" || prepared.Result != "applied" || prepared.PlanHash != hex.EncodeToString(prepareSum[:]) {
		t.Fatalf("prepare output=%s err=%v", prepareOut.String(), err)
	}
	var audit struct {
		Evidence []byte `gorm:"column:evidence"`
	}
	if err := db.Table("broker_route_change_events").Select("evidence").Where("change_id=? AND stage='operator_apply'", changeID).Take(&audit).Error; err != nil {
		t.Fatal("read prepare operator audit")
	}
	var evidence struct {
		Operation string `json:"operation"`
		Actor     string `json:"actor"`
		PlanSHA   string `json:"plan_sha256"`
	}
	if err := json.Unmarshal(audit.Evidence, &evidence); err != nil || evidence.Operation != "prepare" || evidence.Actor != "operator-test" || evidence.PlanSHA != hex.EncodeToString(prepareSum[:]) {
		t.Fatalf("prepare operator audit=%s err=%v", string(audit.Evidence), err)
	}
	var frozen struct {
		Actor   string `gorm:"column:actor"`
		PlanSHA string `gorm:"column:plan_sha256"`
		Phase   string `gorm:"column:phase"`
	}
	if err := db.Table("broker_route_changes").Select("actor,plan_sha256,phase").Where("id=?", changeID).Take(&frozen).Error; err != nil {
		t.Fatal("read original freeze audit")
	}
	if frozen.Actor != "operator-test" || frozen.PlanSHA != hash || frozen.Phase != "connecting" {
		t.Fatalf("original freeze audit changed: actor=%q plan_sha=%q phase=%q", frozen.Actor, frozen.PlanSHA, frozen.Phase)
	}
}

func cutoverDSNWithSchema(dsn, schema string) string {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, err := url.Parse(dsn)
		if err == nil {
			q := u.Query()
			q.Set("search_path", schema)
			u.RawQuery = q.Encode()
			return u.String()
		}
	}
	return dsn + " search_path=" + schema
}

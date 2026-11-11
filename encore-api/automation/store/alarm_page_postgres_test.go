package store_test

import (
	"context"
	"encore.app/automation/store"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/stdlib"
	"os"
	"strings"
	"testing"
	"time"
)

func TestAlarmPageFiltersBeforePagination(t *testing.T) {
	dsn := os.Getenv("AUTOMATION_RECEIPT_TEST_DSN")
	if dsn == "" {
		t.Skip("isolated automation fixture not configured")
	}
	config, err := automationReceiptFixtureConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	admin := stdlib.OpenDB(*config)
	defer admin.Close()
	schema := "alarm_page_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = admin.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec("DROP SCHEMA " + schema + " CASCADE")
	config.RuntimeParams["search_path"] = schema
	db := stdlib.OpenDB(*config)
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE alarm_episodes (
 id text PRIMARY KEY, tenant_id text NOT NULL, policy_id text, policy_key text,
 subject_id text, dimension text, state text, level text, source text,
 revision int, epoch bigint, opened_at timestamptz, first_seq bigint,last_seq bigint,sample_count bigint,
 last_evidence jsonb,acked_at timestamptz,acked_by text,cleared_at timestamptz,updated_at timestamptz)`)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 26; i++ {
		tenant := "tenant-a"
		if i == 25 {
			tenant = "tenant-b"
		}
		level := "H"
		if i < 5 {
			level = "L"
		}
		_, err = db.Exec(`INSERT INTO alarm_episodes VALUES($1,$2,'policy','key','device','','active',$3,'detector',1,1,$4,1,1,1,NULL,NULL,NULL,NULL,$4)`, fmt.Sprintf("%02d", i), tenant, level, base.Add(time.Duration(i)*time.Minute))
		if err != nil {
			t.Fatal(err)
		}
	}
	s := store.New(db)
	ctx := context.Background()
	items, total, err := s.ListEpisodesPage(ctx, "tenant-a", store.EpisodeFilter{Limit: 10, Offset: 10})
	if err != nil || total != 25 || len(items) != 10 || items[0].ID != "14" {
		t.Fatalf("page2: count=%d items=%v err=%v", total, items, err)
	}
	from, to := base.Add(2*time.Minute), base.Add(4*time.Minute)
	items, total, err = s.ListEpisodesPage(ctx, "tenant-a", store.EpisodeFilter{Limit: 2, Level: "L", StartTime: &from, EndTime: &to})
	if err != nil || total != 3 || len(items) != 2 || items[0].ID != "04" {
		t.Fatalf("filtered page: count=%d items=%v err=%v", total, items, err)
	}
	items, total, err = s.ListEpisodesPage(ctx, "tenant-a", store.EpisodeFilter{Limit: 10, Offset: 100})
	if err != nil || total != 25 || len(items) != 0 {
		t.Fatalf("empty page total=%d len=%d err=%v", total, len(items), err)
	}
}

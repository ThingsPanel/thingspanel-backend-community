package app

import (
	"context"
	"errors"
	"testing"

	"encore.app/thingmodel/core"
)

func deletePrincipal(tenant, subject string) core.Principal {
	p := writerPrincipal(tenant, subject)
	p.Capabilities = append(p.Capabilities, string(core.CapabilityDelete))
	return p
}

func TestDeleteRemovesCatalogEntryButRetainsPublishedSnapshot(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepository()
	service := New(repo, sequenceIDs())
	p := deletePrincipal("tenant-a", "user-a")
	created, err := service.Create(ctx, p, "create-delete-model", core.Model{Name: "Delete me"})
	if err != nil {
		t.Fatal(err)
	}
	id := core.ModelID(created.ModelID)
	if _, err := service.Publish(ctx, p, "publish-delete-model", id, created.DraftRevision, "v1"); err != nil {
		t.Fatal(err)
	}
	version, err := service.Version(ctx, p, id, 1)
	if err != nil {
		t.Fatal(err)
	}
	out, err := service.Delete(ctx, p, "delete-model-key", id, created.DraftRevision)
	if err != nil || out.Status != "DELETED" {
		t.Fatalf("delete = %+v, %v", out, err)
	}
	if _, err := service.Get(ctx, p, id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted get = %v", err)
	}
	if _, err := service.Resources(ctx, p, id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted resources = %v", err)
	}
	if _, err := service.Versions(ctx, p, id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted versions menu = %v", err)
	}
	rows, total, err := service.List(ctx, p, 1, 20, "")
	if err != nil || total != 0 || len(rows) != 0 {
		t.Fatalf("list = %v,%d,%v", rows, total, err)
	}
	kept, err := service.Version(ctx, p, id, 1)
	if err != nil || kept.Snapshot.Checksum != version.Snapshot.Checksum {
		t.Fatalf("pinned version = %+v,%v", kept, err)
	}
	audits, events := len(repo.audits), len(repo.events)
	replay, err := service.Delete(ctx, p, "delete-model-key", id, created.DraftRevision)
	if err != nil || !replay.Replayed || replay.Status != "DELETED" {
		t.Fatalf("replay = %+v,%v", replay, err)
	}
	if len(repo.audits) != audits || len(repo.events) != events {
		t.Fatal("replay duplicated deletion effects")
	}
	if _, err := service.Delete(ctx, p, "delete-model-key", id, 2); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed replay = %v", err)
	}
	if _, err := service.Delete(ctx, p, "another-delete-key", id, 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("new deletion of missing model = %v", err)
	}
}

func TestDeleteRejectsStaleRevisionCrossTenantAndMissingCapability(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepository()
	service := New(repo, sequenceIDs())
	p := deletePrincipal("tenant-a", "user-a")
	created, err := service.Create(ctx, p, "create-delete-guards", core.Model{Name: "Keep me"})
	if err != nil {
		t.Fatal(err)
	}
	id := core.ModelID(created.ModelID)
	for _, tc := range []struct {
		name     string
		p        core.Principal
		revision uint64
		want     error
	}{
		{"stale", p, 2, ErrConflict},
		{"zero", p, 0, ErrInvalid},
		{"foreign", deletePrincipal("tenant-b", "user-b"), 1, ErrNotFound},
		{"no delete capability", writerPrincipal("tenant-a", "user-a"), 1, core.ErrUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := service.Delete(ctx, tc.p, "guard-delete-key", id, tc.revision); !errors.Is(err, tc.want) {
				t.Fatalf("delete error = %v, want %v", err, tc.want)
			}
		})
	}
	if _, err := service.Get(ctx, p, id); err != nil {
		t.Fatal(err)
	}
	if len(repo.audits) != 1 || len(repo.events) != 1 {
		t.Fatal("failed deletion wrote effects")
	}
}

func TestDeleteFailuresRollBackAuditEventAndRemoval(t *testing.T) {
	for _, stage := range []string{"audit", "event", "delete"} {
		t.Run(stage, func(t *testing.T) {
			ctx := context.Background()
			repo := newMemoryRepository()
			service := New(repo, sequenceIDs())
			p := deletePrincipal("tenant-a", "user-a")
			created, err := service.Create(ctx, p, "create-delete-rollback", core.Model{Name: "Keep"})
			if err != nil {
				t.Fatal(err)
			}
			id := core.ModelID(created.ModelID)
			repo.failAt = stage
			if _, err := service.Delete(ctx, p, "rollback-delete-key", id, 1); err == nil {
				t.Fatal("expected failure")
			}
			if _, err := service.Get(ctx, p, id); err != nil {
				t.Fatal("deletion was not rolled back", err)
			}
			if len(repo.audits) != 1 || len(repo.events) != 1 {
				t.Fatal("failed deletion left effects")
			}
			repo.failAt = ""
			if out, err := service.Delete(ctx, p, "rollback-delete-key", id, 1); err != nil || out.Replayed {
				t.Fatalf("retry = %+v,%v", out, err)
			}
		})
	}
}

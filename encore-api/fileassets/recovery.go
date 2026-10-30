package fileassets

import (
	"context"
	"time"

	"encore.dev/beta/errs"
	"encore.dev/cron"
	"encore.dev/rlog"
	"encore.dev/storage/objects"
	"encore.dev/storage/sqldb"
	"errors"
)

var _ = cron.NewJob("recover-file-asset-uploads", cron.JobConfig{
	Title:    "Recover interrupted private file asset uploads",
	Every:    15 * cron.Minute,
	Endpoint: RecoverInterruptedUploads,
})

// RecoverInterruptedUploads is private operational maintenance, not a public
// metadata operation. Row locks exclude uploads still running on another replica.
//
//encore:api private
func (s *Service) RecoverInterruptedUploads(ctx context.Context) error {
	if err := s.requireStorage(); err != nil {
		return err
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return assetUnavailable()
	}
	defer tx.Rollback()
	now, err := databaseNow(ctx, tx)
	if err != nil {
		return err
	}
	intents, err := ListPendingUploadIntentsTx(ctx, tx, now.Add(-30*time.Minute), 100)
	if err != nil {
		return assetUnavailable()
	}
	for _, intent := range intents {
		if _, err := tx.Exec(ctx, `SELECT set_config('app.current_tenant_id',$1,true)`, intent.TenantID); err != nil {
			return assetUnavailable()
		}
		if err := s.recoverUploadIntentTx(ctx, tx, intent); err != nil {
			// Do not print signed capabilities, object keys or SDK errors.
			rlog.Error("file asset recovery failed", "asset_id", intent.ID)
			return assetUnavailable()
		}
	}
	if err := tx.Commit(); err != nil {
		return assetUnavailable()
	}
	return nil
}

func (s *Service) recoverUploadIntent(ctx context.Context, intent UploadIntent) error {
	tx, err := BeginTenantScopedTx(ctx, intent.TenantID)
	if err != nil {
		return assetUnavailable()
	}
	defer tx.Rollback()
	if err := LockUploadIntentTx(ctx, tx, intent.TenantID, intent.ID); err != nil {
		return assetUnavailable()
	}
	if err := s.recoverUploadIntentTx(ctx, tx, intent); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return assetUnavailable()
	}
	return nil
}

func (s *Service) recoverUploadIntentTx(ctx context.Context, tx *sqldb.Tx, intent UploadIntent) error {
	asset, err := LoadTx(ctx, tx, intent.TenantID, intent.ID)
	if err == nil {
		if asset.ObjectKey != intent.ObjectKey {
			return assetUnavailable()
		}
		return MarkUploadIntentTx(ctx, tx, intent.TenantID, intent.ID, "committed")
	}
	if errs.Code(err) != errs.NotFound {
		return assetUnavailable()
	}
	if err := s.objects.Delete(ctx, intent.ObjectKey); err != nil && !errors.Is(err, objects.ErrObjectNotFound) {
		return assetUnavailable()
	}
	return MarkUploadIntentTx(ctx, tx, intent.TenantID, intent.ID, "cleaned")
}

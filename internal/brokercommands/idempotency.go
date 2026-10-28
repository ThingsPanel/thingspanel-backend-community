package brokercommands

import (
	"context"
	"crypto/sha256"
	"errors"
	"gorm.io/gorm"
)

// FindIdempotent resolves a retry before preparing a new command, including
// after its expiry/configuration changes. Callers must recheck current access.
func (s *Store) FindIdempotent(ctx context.Context, tenant, caller, intent, key string, canonical []byte) (Row, bool, error) {
	if s == nil || s.db == nil || ctx == nil || tenant == "" || caller == "" || intent == "" || key == "" || len(key) > 256 || len(canonical) == 0 || len(canonical) > maxField {
		return Row{}, false, ErrInvalid
	}
	var row Row
	err := s.db.WithContext(ctx).Where("tenant_id=? AND caller_id=? AND intent=? AND idempotency_key=?", tenant, caller, intent, key).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return Row{}, false, nil
	}
	if err != nil {
		return Row{}, false, err
	}
	digest := sha256.Sum256(canonical)
	if !equalDigest(row.CanonicalSHA256, digest[:]) {
		return Row{}, false, ErrConflict
	}
	return row, true, nil
}

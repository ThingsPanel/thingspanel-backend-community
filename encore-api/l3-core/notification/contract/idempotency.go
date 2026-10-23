package contract

// UpdateReplayRecord is the persisted result found for an idempotency key
// scoped to a tenant and operation. Storage must enforce that scope atomically.
type UpdateReplayRecord struct {
	RequestDigest string
	ResourceID    string
	Version       int64
}

type UpdateDecision uint8

const (
	UpdateApply UpdateDecision = iota
	UpdateReplay
	UpdateIdempotencyConflict
	UpdateVersionConflict
)

// DecideUpdate makes the required ordering explicit: an existing idempotency
// key is checked before expectedVersion, so a valid replay survives later
// resource versions. This policy helper does not replace transactional storage
// uniqueness or compare-and-swap enforcement.
func DecideUpdate(existing *UpdateReplayRecord, requestDigest string, expectedVersion, currentVersion int64) UpdateDecision {
	if existing != nil {
		if existing.RequestDigest == requestDigest {
			return UpdateReplay
		}
		return UpdateIdempotencyConflict
	}
	if expectedVersion != currentVersion {
		return UpdateVersionConflict
	}
	return UpdateApply
}

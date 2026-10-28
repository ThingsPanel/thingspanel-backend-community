package contract

// SourceProjectionIndex represents records already loaded for one authorized
// source tuple. The repository must select/lock this tuple transactionally;
// this helper only defines the deterministic replay and history-cap decision.
type SourceProjectionIndex struct {
	NotificationGroupID string
	Revisions           map[int64]SourceGroupProjection
}

type SourceProjectionDecision uint8

const (
	SourceProjectionCreate SourceProjectionDecision = iota
	SourceProjectionReplay
	SourceProjectionConflict
	SourceProjectionHistoryLimit
)

func DecideSourceProjection(existing *SourceProjectionIndex, request SourceGroupProjectionRequest) SourceProjectionDecision {
	if existing == nil {
		return SourceProjectionCreate
	}
	if existing.NotificationGroupID != request.NotificationGroupID {
		return SourceProjectionConflict
	}
	if record, ok := existing.Revisions[request.GroupRevision]; ok {
		if record.NotificationGroupID == request.NotificationGroupID {
			return SourceProjectionReplay
		}
		return SourceProjectionConflict
	}
	if len(existing.Revisions) >= MaxSourceGroupProjectionRevisions {
		return SourceProjectionHistoryLimit
	}
	return SourceProjectionCreate
}

func LatestSourceProjectionRevision(index *SourceProjectionIndex) int64 {
	var latest int64
	if index == nil {
		return latest
	}
	for revision := range index.Revisions {
		if revision > latest {
			latest = revision
		}
	}
	return latest
}

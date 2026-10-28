package automation

import (
	"context"
	"encoding/json"
)

// Inbound idempotency (A27).
//
// The outbound half has existed since A10: every device command and every
// notification carries a stable key into auto_outbox, so a replayed dispatch
// cannot open the same door twice. The inbound half was missing. A client that
// timed out on POST /v1/rules/:id/run and retried got a second run, which is a
// second command — "the assistant retried" became "the door opened twice".
//
// The contract here is the ordinary one: the same request_id returns the first
// response, byte for byte, and does no work the second time. A different
// request_id is a different request. The key is scoped by tenant, because a key
// that is only a hash of the arguments would be shared between two customers
// who typed the same thing.

// requestRetentionHours is how long a key can be replayed.
//
// A retry after a timeout happens within seconds or minutes; a day is generous
// enough for a laptop that slept and short enough that the table stays small.
const requestRetentionHours = 24

// replay returns the response an earlier request with this key produced.
func (s *Service) replay(ctx context.Context, tenantID, key string, into any) bool {
	if key == "" {
		return false
	}
	rt, err := s.rtOrErr()
	if err != nil {
		return false
	}
	raw, ok, err := rt.Store().LookupRequest(ctx, tenantID, key)
	if err != nil || !ok {
		return false
	}
	// A row we cannot decode is treated as absent: doing the work twice is
	// recoverable, refusing a legitimate retry is not.
	return json.Unmarshal(raw, into) == nil
}

// replayRule replays a cached rule response — but only while the rule it
// describes still exists.
//
// "Create it, delete it, create the same thing again" is a real sequence, and
// the second create carries the same arguments and therefore the same key.
// Answering it with a rule the user already deleted would be a ghost: the API
// would report success and the rule list would stay empty.
func (s *Service) replayRule(ctx context.Context, tenantID, key string) (*RuleResponse, bool) {
	var prior RuleResponse
	if !s.replay(ctx, tenantID, key, &prior) {
		return nil, false
	}
	rt, err := s.rtOrErr()
	if err != nil {
		return nil, false
	}
	if _, err := rt.Store().GetRule(ctx, tenantID, prior.ID); err != nil {
		return nil, false
	}
	return &prior, true
}

// remember stores the response so a retry can be answered without redoing it.
//
// A failure to store is deliberately not fatal. The work has already happened
// and the caller is about to be told it succeeded; turning that into an error
// would make the client retry a request that did in fact go through. The cost
// of a lost row is one duplicate on a retry that may never come.
func (s *Service) remember(ctx context.Context, tenantID, key, endpoint string, v any) {
	if key == "" || v == nil {
		return
	}
	rt, err := s.rtOrErr()
	if err != nil {
		return
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return
	}
	_ = rt.Store().SaveRequest(ctx, tenantID, key, endpoint, raw)
}

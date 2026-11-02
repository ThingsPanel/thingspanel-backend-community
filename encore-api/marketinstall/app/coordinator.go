package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

func Digest(value interface{}) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", ErrInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var normalized interface{}
	if err := decoder.Decode(&normalized); err != nil {
		return "", ErrInvalid
	}
	// Marshal sorts map keys while UseNumber preserves integer precision. This is
	// an internal v1 encoding, not a claim to implement JCS or artifact signatures.
	canonical, err := json.Marshal(normalized)
	if err != nil {
		return "", ErrInvalid
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}
func NormalizeRequest(req Request) (Request, error) {
	if strings.TrimSpace(req.BundleKey) == "" || strings.TrimSpace(req.Version) == "" {
		return Request{}, ErrInvalid
	}
	if req.OverwritePolicy == "" {
		req.OverwritePolicy = "default"
	}
	if req.OverwritePolicy != "default" {
		return Request{}, fmt.Errorf("%w: only no-overwrite is supported", ErrInvalid)
	}
	req.DeviceBindings = append([]Binding{}, req.DeviceBindings...)
	sort.Slice(req.DeviceBindings, func(i, j int) bool { return req.DeviceBindings[i].BindingKey < req.DeviceBindings[j].BindingKey })
	for i, b := range req.DeviceBindings {
		if b.BindingKey == "" || b.LocalDeviceID == "" || (i > 0 && req.DeviceBindings[i-1].BindingKey == b.BindingKey) {
			return Request{}, ErrInvalid
		}
	}
	return req, nil
}
func ValidatePlan(plan Plan) error {
	if !strings.HasPrefix(plan.ArtifactDigest, "sha256:") || len(plan.ArtifactDigest) != 71 {
		return ErrInvalid
	}
	if _, err := hex.DecodeString(strings.TrimPrefix(plan.ArtifactDigest, "sha256:")); err != nil {
		return ErrInvalid
	}
	if len(plan.Steps) == 0 || len(plan.Steps) > 256 {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for _, step := range plan.Steps {
		if step.ID == "" || len(step.ID) > 200 || step.ResourceKey == "" || len(step.ResourceKey) > 128 || seen[step.ID] {
			return ErrInvalid
		}
		seen[step.ID] = true
		allowed := (step.Owner == "thingmodel" && (step.Action == "create" || step.Action == "publish" || step.Action == "readback")) || (step.Owner == "community-config" && step.Action == "bind") || (step.Owner == "thingsvis" && step.Action == "import")
		if !allowed {
			return fmt.Errorf("%w: unsupported provider action", ErrInvalid)
		}
		if len(step.Payload) > 2*1024*1024 || !json.Valid(step.Payload) || bytes.Equal(bytes.TrimSpace(step.Payload), []byte("null")) {
			return ErrInvalid
		}
	}
	return nil
}
func StableStepKey(operationID string, step Step) string {
	digest, _ := Digest(struct {
		Version                        int
		Operation, Step, Owner, Action string
	}{1, operationID, step.ID, step.Owner, step.Action})
	return digest
}
func StepHash(step Step) (string, error) {
	return Digest(struct {
		Version int
		Step    Step
	}{1, step})
}

type Coordinator struct {
	Store        Store
	Provider     Provider
	NewID        func() string
	LeaseSeconds int
}

func (c *Coordinator) Submit(ctx context.Context, identity Identity, key string, req Request) (Operation, error) {
	if c.Store == nil || c.Provider == nil || c.NewID == nil || identity.TenantID == "" || identity.SubjectID == "" || identity.AuthorizationRef == "" || len(key) < 8 || len(key) > 128 {
		return Operation{}, ErrInvalid
	}
	normalized, err := NormalizeRequest(req)
	if err != nil {
		return Operation{}, err
	}
	requestDigest, err := Digest(struct {
		Version         int
		Tenant, Subject string
		Request         Request
	}{1, identity.TenantID, identity.SubjectID, normalized})
	if err != nil {
		return Operation{}, err
	}
	// Replays never replace the original plan or credential reference. They are
	// authorized at the transport, and provider permissions are rechecked per step.
	existing, err := c.Store.Get(ctx, identity.TenantID, identity.SubjectID, key)
	if err == nil {
		if existing.RequestDigest != requestDigest {
			return Operation{}, ErrConflict
		}
		return existing, nil
	}
	if !errorsIsNotFound(err) {
		return Operation{}, err
	}
	plan, err := c.Provider.Prepare(ctx, PrepareRequest{ProtocolVersion: 1, Identity: identity, Request: normalized})
	if err != nil {
		return Operation{}, err
	}
	if err := ValidatePlan(plan); err != nil {
		return Operation{}, err
	}
	op := Operation{ID: c.NewID(), TenantID: identity.TenantID, SubjectID: identity.SubjectID, AuthorizationRef: identity.AuthorizationRef, RequestKey: key, RequestDigest: requestDigest, State: Queued, Plan: plan, ArtifactDigest: plan.ArtifactDigest}
	return c.Store.Create(ctx, op)
}
func errorsIsNotFound(err error) bool { return errors.Is(err, ErrNotFound) }

// RunOne commits intent before execution, queries the original key before any
// dispatch, and stops on any uncertain owner or database outcome. A lease can
// expire during HTTP work; its generation cannot confirm or advance afterward.
func (c *Coordinator) RunOne(ctx context.Context) (bool, error) {
	if c.Store == nil || c.Provider == nil {
		return false, ErrInvalid
	}
	lease := c.LeaseSeconds
	if lease < 5 || lease > 300 {
		lease = 60
	}
	work, err := c.Store.Claim(ctx, lease)
	if err != nil || work == nil {
		return false, err
	}
	req := StepRequest{ProtocolVersion: 1, Identity: Identity{TenantID: work.Operation.TenantID, SubjectID: work.Operation.SubjectID, AuthorizationRef: work.Operation.AuthorizationRef}, OperationID: work.Operation.ID, Generation: work.Operation.Generation, StepID: work.Step.ID, IdempotencyKey: work.IdempotencyKey, PayloadHash: work.PayloadHash, Step: work.Step, PriorResults: work.PriorResults}
	observation, err := c.Provider.Query(ctx, req)
	if err != nil {
		return true, c.Store.Hold(ctx, *work, Paused, "OWNER_QUERY_UNKNOWN")
	}
	if observation.State == NotObserved {
		if err := c.Store.Dispatch(ctx, *work); err != nil {
			return true, err
		}
		observation, err = c.Provider.Execute(ctx, req)
		if err != nil {
			return true, c.Store.Hold(ctx, *work, Paused, "OWNER_EXECUTE_UNKNOWN")
		}
	}
	switch observation.State {
	case Confirmed:
		receipt := observation.Receipt
		if receipt == nil || !ValidReceipt(*receipt, work.Step, work.PayloadHash) {
			return true, c.Store.Hold(ctx, *work, Paused, "INVALID_OWNER_RECEIPT")
		}
		return true, c.Store.Confirm(ctx, *work, *receipt)
	case Rejected:
		return true, c.Store.Hold(ctx, *work, Rejected, cleanErrorCode(observation.ErrorCode, "OWNER_REJECTED"))
	case Expired:
		return true, c.Store.Hold(ctx, *work, Paused, "OWNER_RECEIPT_EXPIRED")
	default:
		return true, c.Store.Hold(ctx, *work, Paused, "OWNER_RESULT_UNKNOWN")
	}
}
func cleanErrorCode(code, fallback string) string {
	if code == "" || len(code) > 80 {
		return fallback
	}
	for _, r := range code {
		if !(r == '_' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return fallback
		}
	}
	return code
}

func ValidReceipt(receipt Receipt, step Step, hash string) bool {
	if receipt.Owner != step.Owner || receipt.PayloadHash != hash || receipt.ResourceID == "" || len(receipt.ResourceID) > 512 || len(receipt.Data) > 2*1024*1024 {
		return false
	}
	var data map[string]json.RawMessage
	return json.Unmarshal(receipt.Data, &data) == nil && data != nil
}

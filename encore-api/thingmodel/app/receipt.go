package app

import (
	"context"
	"encoding/hex"
	"fmt"
	"time"

	"encore.app/thingmodel/core"
)

const (
	ReceiptConfirmed   = "confirmed"
	ReceiptNotObserved = "not_observed"
	ReceiptExpired     = "expired"
)

// ReceiptQuery contains trusted identity and the original command identity.
// An absent ExpectedPayloadHash does not change the command's stored hash.
type ReceiptQuery struct {
	TenantID            core.TenantID
	SubjectID           string
	Operation           string
	Key                 string
	ExpectedPayloadHash string
}

// ReceiptObservation reports a committed owner outcome. NotObserved only means
// no committed receipt at this read's serialization point; it never proves that
// an old request cannot arrive later. Expired keys remain reserved tombstones.
type ReceiptObservation struct {
	State         string     `json:"state"`
	Operation     string     `json:"operation"`
	Key           string     `json:"key"`
	PayloadHash   string     `json:"payloadHash,omitempty"`
	Result        *Result    `json:"result,omitempty"`
	RetainedUntil *time.Time `json:"retainedUntil,omitempty"`
}

// ReceiptReader must serialize reads with Execute on the exact same identity
// and read the authoritative owner database, never an eventually consistent cache.
// Database failures are errors, and must not become NotObserved.
type ReceiptReader interface {
	QueryReceipt(context.Context, ReceiptQuery) (ReceiptObservation, error)
}

func ReadReceipt(ctx context.Context, reader ReceiptReader, principal core.Principal, operation, key, expectedHash string) (ReceiptObservation, error) {
	capability, err := ReceiptCapability(operation)
	if err != nil {
		return ReceiptObservation{}, err
	}
	if err := authorize(principal, principal.TenantID, capability); err != nil {
		return ReceiptObservation{}, err
	}
	if err := validateKey(key); err != nil {
		return ReceiptObservation{}, err
	}
	if !validExpectedHash(expectedHash) {
		return ReceiptObservation{}, fmt.Errorf("%w: expected payload hash must be 64 hexadecimal characters", ErrInvalid)
	}
	if reader == nil {
		return ReceiptObservation{}, fmt.Errorf("%w: receipt reader is not configured", ErrInvalid)
	}
	return reader.QueryReceipt(ctx, ReceiptQuery{TenantID: principal.TenantID, SubjectID: principal.SubjectID, Operation: operation, Key: key, ExpectedPayloadHash: expectedHash})
}

func ReceiptCapability(operation string) (core.Capability, error) {
	switch operation {
	case "create", "update", "update-resources":
		return core.CapabilityWrite, nil
	case "publish":
		return core.CapabilityPublish, nil
	default:
		return "", fmt.Errorf("%w: unsupported receipt operation", ErrInvalid)
	}
}

func validExpectedHash(value string) bool {
	if value == "" {
		return true
	}
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

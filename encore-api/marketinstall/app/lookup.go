package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// OperationKeyReader deliberately exposes no submission or progress mutation.
// The same owner ledger remains authoritative after a submit reply is lost.
type OperationKeyReader interface {
	Get(context.Context, string, string, string) (Operation, error)
}

type OperationLookup struct {
	CacheControl string     `json:"-" header:"Cache-Control"`
	State        string     `json:"state"`
	OperationKey string     `json:"operationKey"`
	Operation    *Operation `json:"operation,omitempty"`
}

// LookupByKey observes only the current tenant and original subject's record.
// NotObserved means this read did not find a committed record. A concurrent
// submit can still be in flight; this result never authorizes another submit.
// Reading does not renew the old execution grant or resume an installation.
func LookupByKey(ctx context.Context, ledger OperationKeyReader, identity Identity, key string) (OperationLookup, error) {
	if ledger == nil || strings.TrimSpace(identity.TenantID) == "" || strings.TrimSpace(identity.SubjectID) == "" || len(key) < 8 || len(key) > 128 || strings.ContainsRune(key, '\x00') {
		return OperationLookup{}, ErrInvalid
	}
	op, err := ledger.Get(ctx, identity.TenantID, identity.SubjectID, key)
	if errors.Is(err, ErrNotFound) {
		return OperationLookup{CacheControl: "no-store", State: NotObserved, OperationKey: key}, nil
	}
	if err != nil {
		return OperationLookup{}, err
	}
	if op.ID == "" || op.BundleKey == "" || op.Version == "" || op.TenantID != identity.TenantID || op.SubjectID != identity.SubjectID || op.RequestKey != key {
		return OperationLookup{}, fmt.Errorf("installation lookup identity is inconsistent")
	}
	switch op.State {
	case Queued, Running, Paused, Completed, Rejected:
	default:
		return OperationLookup{}, fmt.Errorf("installation lookup state is inconsistent")
	}
	return OperationLookup{CacheControl: "no-store", State: Confirmed, OperationKey: key, Operation: &op}, nil
}

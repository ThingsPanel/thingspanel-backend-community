package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type lookupReaderFunc func(context.Context, string, string, string) (Operation, error)

func (read lookupReaderFunc) Get(ctx context.Context, tenant, subject, key string) (Operation, error) {
	return read(ctx, tenant, subject, key)
}

func TestLookupByKeyIsScopedReadOnlyObservation(t *testing.T) {
	identity := Identity{TenantID: "tenant", SubjectID: "subject"}
	key := "original/key + ? #"
	operation := Operation{ID: "original-operation", BundleKey: "original-bundle", Version: "1", TenantID: identity.TenantID, SubjectID: identity.SubjectID, RequestKey: key, State: Queued, AuthorizationRef: "private-original-grant", RequestDigest: strings.Repeat("b", 64)}
	calls := 0
	reader := lookupReaderFunc(func(_ context.Context, tenant, subject, requested string) (Operation, error) {
		calls++
		if tenant != identity.TenantID || subject != identity.SubjectID || requested != key {
			t.Fatal("original subject, tenant or opaque key was substituted")
		}
		return operation, nil
	})
	result, err := LookupByKey(context.Background(), reader, identity, key)
	if err != nil || calls != 1 || result.State != Confirmed || result.OperationKey != key || result.Operation == nil || result.Operation.ID != operation.ID || result.Operation.State != Queued {
		t.Fatalf("read result=%#v calls=%d error=%v", result, calls, err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"private-original-grant", operation.RequestDigest, `"plan"`, `"tenantId"`, `"subjectId"`} {
		if strings.Contains(string(encoded), secret) {
			t.Fatal("lookup exposed private execution identity or plan")
		}
	}
}

func TestLookupByKeyDistinguishesAbsenceFromUnavailable(t *testing.T) {
	identity := Identity{TenantID: "tenant", SubjectID: "subject"}
	for _, fault := range []error{ErrNotFound, context.Canceled, errors.New("database unavailable")} {
		result, err := LookupByKey(context.Background(), lookupReaderFunc(func(context.Context, string, string, string) (Operation, error) {
			return Operation{}, fault
		}), identity, "original-key")
		if errors.Is(fault, ErrNotFound) {
			if err != nil || result.State != NotObserved || result.Operation != nil || result.OperationKey != "original-key" {
				t.Fatalf("absent receipt: %#v %v", result, err)
			}
		} else if !errors.Is(err, fault) || result.State != "" || result.Operation != nil {
			t.Fatalf("unavailable became an observation: %#v %v", result, err)
		}
	}
}

func TestLookupByKeyRejectsInvalidIdentityAndInconsistentOwnerResult(t *testing.T) {
	identity := Identity{TenantID: "tenant", SubjectID: "subject"}
	for _, test := range []struct {
		identity Identity
		key      string
	}{
		{Identity{}, "original-key"}, {Identity{TenantID: "tenant"}, "original-key"},
		{identity, "short"}, {identity, strings.Repeat("x", 129)}, {identity, "original\x00key"},
	} {
		_, err := LookupByKey(context.Background(), lookupReaderFunc(func(context.Context, string, string, string) (Operation, error) {
			t.Fatal("invalid query reached the ledger")
			return Operation{}, nil
		}), test.identity, test.key)
		if !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid query accepted: %v", err)
		}
	}
	valid := Operation{ID: "original", BundleKey: "bundle", Version: "1", TenantID: "tenant", SubjectID: "subject", RequestKey: "original-key", State: Queued}
	for _, change := range []func(*Operation){
		func(op *Operation) { op.TenantID = "other" }, func(op *Operation) { op.SubjectID = "other" },
		func(op *Operation) { op.RequestKey = "another-key" }, func(op *Operation) { op.State = "unknown-state" },
		func(op *Operation) { op.BundleKey = "" }, func(op *Operation) { op.ID = "" },
	} {
		op := valid
		change(&op)
		result, err := LookupByKey(context.Background(), lookupReaderFunc(func(context.Context, string, string, string) (Operation, error) { return op, nil }), identity, "original-key")
		if err == nil || result.Operation != nil || result.State != "" {
			t.Fatalf("inconsistent lookup accepted: %#v %v", result, err)
		}
	}
}

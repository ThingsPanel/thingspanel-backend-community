package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"encore.app/thingmodel/core"
)

type receiptReaderFunc func(context.Context, ReceiptQuery) (ReceiptObservation, error)

func (f receiptReaderFunc) QueryReceipt(ctx context.Context, q ReceiptQuery) (ReceiptObservation, error) {
	return f(ctx, q)
}

func TestReceiptQueryUsesTrustedIdentityAndCurrentCapability(t *testing.T) {
	principal := writerPrincipal("tenant-a", "subject-a")
	called := 0
	reader := receiptReaderFunc(func(ctx context.Context, q ReceiptQuery) (ReceiptObservation, error) {
		called++
		if q.TenantID != principal.TenantID || q.SubjectID != principal.SubjectID || q.Operation != "create" || q.Key != "original-key" || q.ExpectedPayloadHash != strings.Repeat("a", 64) {
			t.Fatalf("query changed identity: %#v", q)
		}
		return ReceiptObservation{State: ReceiptNotObserved, Operation: q.Operation, Key: q.Key}, nil
	})
	observation, err := ReadReceipt(context.Background(), reader, principal, "create", "original-key", strings.Repeat("a", 64))
	if err != nil || observation.State != ReceiptNotObserved || called != 1 {
		t.Fatalf("query=%#v calls=%d err=%v", observation, called, err)
	}
	denied := principal
	denied.Capabilities = []string{string(core.CapabilityRead)}
	if _, err := ReadReceipt(context.Background(), reader, denied, "create", "original-key", ""); !errors.Is(err, core.ErrUnauthorized) {
		t.Fatalf("revoked writer queried receipt: %v", err)
	}
	for _, operation := range []string{"delete", "prepare", "activate", "revoke", ""} {
		if _, err := ReadReceipt(context.Background(), reader, principal, operation, "original-key", ""); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid operation %s: %v", operation, err)
		}
	}
	for _, key := range []string{"short", strings.Repeat("x", 129)} {
		if _, err := ReadReceipt(context.Background(), reader, principal, "create", key, ""); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid key accepted: %v", err)
		}
	}
	for _, hash := range []string{"x", strings.Repeat("z", 64)} {
		if _, err := ReadReceipt(context.Background(), reader, principal, "create", "original-key", hash); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid hash accepted: %v", err)
		}
	}
	if called != 1 {
		t.Fatal("invalid or unauthorized query touched storage")
	}
}

func TestReceiptQueryPersistenceFailureDoesNotBecomeNotObserved(t *testing.T) {
	unavailable := errors.New("owner database unavailable")
	reader := receiptReaderFunc(func(context.Context, ReceiptQuery) (ReceiptObservation, error) {
		return ReceiptObservation{}, unavailable
	})
	observation, err := ReadReceipt(context.Background(), reader, writerPrincipal("tenant", "subject"), "create", "original-key", "")
	if !errors.Is(err, unavailable) || observation.State != "" {
		t.Fatalf("error became observation: %#v %v", observation, err)
	}
}

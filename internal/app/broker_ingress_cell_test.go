package app

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"project/internal/brokerledger"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

type cellProcessStore struct {
	legacyCalls atomic.Int32
	cellCalls   atomic.Int32
	cell        string
}

func (s *cellProcessStore) Accept(context.Context, brokerledger.Message) (string, bool, error) {
	return "", false, nil
}
func (s *cellProcessStore) ProcessOne(context.Context, func(context.Context, *gorm.DB, brokerledger.Message) error) (bool, error) {
	s.legacyCalls.Add(1)
	return false, nil
}
func (s *cellProcessStore) ProcessOneInCell(_ context.Context, cell string, _ func(context.Context, *gorm.DB, brokerledger.Message) error) (bool, error) {
	s.cellCalls.Add(1)
	s.cell = cell
	return true, nil
}

func TestBrokerIngressWorkerConfiguredCellUsesScopedStoreAPI(t *testing.T) {
	store := &cellProcessStore{}
	worker := &brokerIngressWorker{cell: "cell-a", store: store, processor: ingressProcessorFunc(func(context.Context, *gorm.DB, brokerledger.Message) error { return nil }), logger: logrus.New()}
	processed, err := worker.processOne(context.Background())
	if err != nil || !processed || store.cellCalls.Load() != 1 || store.cell != "cell-a" || store.legacyCalls.Load() != 0 {
		t.Fatalf("cell worker result processed=%v err=%v scoped=%d cell=%q legacy=%d", processed, err, store.cellCalls.Load(), store.cell, store.legacyCalls.Load())
	}
}

func TestBrokerIngressWorkerConfiguredCellFailsClosedWithoutScopedStoreAPI(t *testing.T) {
	store := &testIngressStore{}
	worker := &brokerIngressWorker{cell: "cell-a", store: store, processor: ingressProcessorFunc(func(context.Context, *gorm.DB, brokerledger.Message) error { return nil }), logger: logrus.New()}
	processed, err := worker.processOne(context.Background())
	if processed || err == nil || err.Error() != "reliable ingress store does not support cell scoping" {
		t.Fatalf("unsupported scoped API result processed=%v err=%v", processed, err)
	}
	if store.processCalls.Load() != 0 {
		t.Fatalf("worker fell back to unscoped ProcessOne %d times", store.processCalls.Load())
	}
	if errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected cancellation: %v", err)
	}
}

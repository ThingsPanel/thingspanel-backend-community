package app

import (
	"context"
	"gorm.io/gorm"
	"project/internal/brokerledger"
	"testing"
)

type preparedIngressProbe struct{ prepared, unprepared int }

func (p *preparedIngressProbe) Process(context.Context, *gorm.DB, brokerledger.Message) error {
	p.unprepared++
	return nil
}
func (p *preparedIngressProbe) Prepare(context.Context, *gorm.DB, brokerledger.Message) (*brokerledger.PreparedOperation, error) {
	p.prepared++
	return &brokerledger.PreparedOperation{Write: func(context.Context, *gorm.DB, brokerledger.Message) error { return nil }}, nil
}

type preparedStoreProbe struct {
	testIngressStore
	cell string
}

func (s *preparedStoreProbe) ProcessOnePrepared(ctx context.Context, cell string, prepare brokerledger.PrepareHandler) (bool, error) {
	s.cell = cell
	_, err := prepare(ctx, nil, brokerledger.Message{})
	return true, err
}

func TestBrokerIngressPreparedAssemblyNeverFallsBackToUnvalidatedProcess(t *testing.T) {
	p := &preparedIngressProbe{}
	unsupported := &testIngressStore{}
	worker := &brokerIngressWorker{cell: "qa-cell", store: unsupported, processor: p}
	if worked, err := worker.processOne(context.Background()); worked || err == nil || p.unprepared != 0 || unsupported.processCalls.Load() != 0 {
		t.Fatal("prepared processor downgraded to legacy store")
	}
	supported := &preparedStoreProbe{}
	worker.store = supported
	if worked, err := worker.processOne(context.Background()); !worked || err != nil || p.prepared != 1 || p.unprepared != 0 || supported.cell != "qa-cell" {
		t.Fatalf("prepared assembly failed: %v %v %+v", worked, err, p)
	}
}

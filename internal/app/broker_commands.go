package app

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"gorm.io/gorm"
	"project/internal/adapter/mqttadapter"
	"project/internal/brokercommands"
	"project/internal/service"
	"project/pkg/global"
	"sync"
	"time"
)

type brokerCommandWorker struct {
	store       *brokercommands.Store
	adapter     *mqttadapter.Adapter
	logger      *logrus.Logger
	cancel      context.CancelFunc
	done        chan struct{}
	stopOnce    sync.Once
	runtimeSync func(context.Context) error
}

func commandGuard(ctx context.Context, tx *gorm.DB, row brokercommands.Row) error {
	if row.RouteCell != viper.GetString("mqtt.broker_cell") {
		return brokercommands.ErrRejected
	}
	if err := service.ValidateDurableCommandCaller(ctx, tx, row); err != nil {
		return err
	}
	return service.ValidateDurableCommandSnapshot(ctx, tx, row)
}
func startBrokerCommandWorker(ctx context.Context, adapter *mqttadapter.Adapter, logger *logrus.Logger) (*brokerCommandWorker, error) {
	if viper.GetString("mqtt.input_profile") != "ingress_v1" {
		return nil, nil
	}
	if global.DB == nil || adapter == nil {
		return nil, errors.New("durable command dependencies unavailable")
	}
	if !global.DB.Migrator().HasTable("broker_command_requests") {
		return nil, errors.New("durable command migration 006 is required")
	}
	if err := service.ExpectedDataIngressSchemaReady(global.DB); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	w := &brokerCommandWorker{store: brokercommands.New(global.DB), adapter: adapter, logger: logger, cancel: cancel, done: make(chan struct{})}
	runtimeSync, err := brokerRuntimeSync(global.DB, "dispatcher")
	if err != nil {
		cancel()
		return nil, err
	}
	w.runtimeSync = runtimeSync
	if runtimeSync != nil {
		if err := runtimeSync(ctx); err != nil {
			cancel()
			return nil, err
		}
	}
	go w.run(ctx)
	return w, nil
}
func (w *brokerCommandWorker) Stop() {
	if w != nil {
		w.stopOnce.Do(func() { w.cancel(); <-w.done })
	}
}
func (w *brokerCommandWorker) run(ctx context.Context) {
	defer close(w.done)
	sweep := time.Now().Add(-time.Minute)
	var lastSync, lastExpected time.Time
	for ctx.Err() == nil {
		if w.runtimeSync != nil && time.Since(lastSync) >= 5*time.Second {
			if err := w.runtimeSync(ctx); err != nil {
				if ctx.Err() != nil {
					return
				}
				w.logger.Warn("command dispatch paused: route runtime synchronization unavailable")
				timer := time.NewTimer(time.Second)
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
				continue
			}
			lastSync = time.Now()
		}
		if time.Since(sweep) >= time.Minute {
			if _, err := w.store.MarkStaleDispatches(ctx, 30*time.Second); err != nil && ctx.Err() == nil {
				w.logger.WithError(err).Warn("durable command stale sweep failed")
			}
			sweep = time.Now()
		}
		if time.Since(lastExpected) >= 5*time.Second {
			expectedCtx, expectedCancel := context.WithTimeout(ctx, 2*time.Second)
			err := service.DispatchPendingExpected(expectedCtx, viper.GetString("mqtt.broker_cell"))
			expectedCancel()
			if err != nil && ctx.Err() == nil {
				w.logger.WithError(err).Warn("pending expected message dispatch deferred")
			}
			lastExpected = time.Now()
		}
		row, err := w.store.ClaimInCell(ctx, viper.GetString("mqtt.broker_cell"), commandGuard)
		if err == nil && row != nil {
			err = w.store.Dispatch(ctx, row.ID, commandGuard, func(pubCtx context.Context, current brokercommands.Row) error {
				var prepared brokercommands.PreparedCommand
				if err := json.Unmarshal(current.Payload, &prepared); err != nil {
					return err
				}
				prepared.Message.MessageID = current.ID
				return w.adapter.PublishDurableCommand(pubCtx, current, &prepared.Message, prepared.Encoded)
			})
		}
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			w.logger.WithError(err).Warn("durable command dispatch paused; no automatic publish replay")
		}
		if row != nil && err == nil {
			continue
		}
		delay := 100 * time.Millisecond
		if err != nil {
			delay = time.Second
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

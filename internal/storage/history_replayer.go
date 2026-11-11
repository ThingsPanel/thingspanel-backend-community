package storage

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// historyReplayer drains durable history intents into the configured sink. It
// stops claiming when cancelled; whatever it holds stays pending and is replayed
// after the lease expires, so stopping never loses history.
type historyReplayer struct {
	db       *gorm.DB
	logger   Logger
	sink     HistorySink
	deleter  HistoryDeleter // nil when the sink cannot delete a device's history
	owner    string
	interval time.Duration
	batch    int
	lease    time.Duration

	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once
}

func newHistoryReplayer(db *gorm.DB, logger Logger, config Config) *historyReplayer {
	sink := config.HistorySink
	if sink == nil {
		sink = NewPostgresHistorySink(db)
	}
	deleter, _ := sink.(HistoryDeleter)
	r := &historyReplayer{deleter: deleter, db: db, logger: logger, sink: sink, owner: "replayer-" + uuid.NewString(),
		interval: 500 * time.Millisecond, batch: 200, lease: 60 * time.Second, done: make(chan struct{})}
	if config.HistoryReplayIntervalMillis > 0 {
		r.interval = time.Duration(config.HistoryReplayIntervalMillis) * time.Millisecond
	}
	if config.HistoryReplayBatch > 0 {
		r.batch = config.HistoryReplayBatch
	}
	if config.HistoryReplayLeaseSeconds > 0 {
		r.lease = time.Duration(config.HistoryReplayLeaseSeconds) * time.Second
	}
	return r
}

func (r *historyReplayer) start(ctx context.Context) {
	ctx, r.cancel = context.WithCancel(ctx)
	go r.run(ctx)
}

func (r *historyReplayer) run(ctx context.Context) {
	defer close(r.done)
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		// Drain until empty or failing, then wait for the next tick.
		for ctx.Err() == nil {
			n, err := ReplayHistoryIntents(ctx, r.db, r.sink, r.owner, r.batch, r.lease)
			if err != nil {
				if ctx.Err() == nil {
					r.logger.Errorf("history replay failed; intents stay pending: %v", err)
				}
				break
			}
			if n == 0 {
				break
			}
		}
		for r.deleter != nil && ctx.Err() == nil {
			n, err := ReplayHistoryDeletions(ctx, r.db, r.deleter, r.owner, r.batch, r.lease)
			if err != nil {
				if ctx.Err() == nil {
					r.logger.Errorf("history deletion failed; it stays pending: %v", err)
				}
				break
			}
			if n == 0 {
				break
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (r *historyReplayer) stop(timeout time.Duration) {
	r.once.Do(func() {
		if r.cancel == nil {
			return
		}
		r.cancel()
		select {
		case <-r.done:
		case <-time.After(timeout):
			r.logger.Warn("history replayer stop timeout")
		}
	})
}

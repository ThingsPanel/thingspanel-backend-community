package mqttadapter

import (
	"context"
	"errors"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/sirupsen/logrus"
	bc "project/internal/brokercontract"
)

const targetPublisherLimit = 32
const targetPublisherIdle = 30 * time.Second
const targetPublisherLifetime = 45 * time.Second

var errTargetPublisherBusy = errors.New("target publisher busy")

type targetPublisherEntry struct {
	client        mqtt.Client
	created, used time.Time
	busy          bool
}
type targetPublisherPool struct {
	mu      sync.Mutex
	ctx     context.Context
	config  MQTTConfig
	logger  *logrus.Logger
	entries map[string]*targetPublisherEntry
	closed  bool
	limit   int
	now     func() time.Time
	connect func(context.Context, MQTTConfig) (mqtt.Client, error)
}

func (a *Adapter) ConfigureTargetPublisher(ctx context.Context, config MQTTConfig) error {
	if ctx == nil || config.ClientID == "" || config.Username == "" || config.Password == "" {
		return errors.New("target publisher requires lifecycle and explicit service credentials")
	}
	if _, err := bc.TargetPublisherClientID(config.ClientID, "validation-target"); err != nil {
		return err
	}
	p := &targetPublisherPool{ctx: ctx, config: config, logger: a.logger, entries: make(map[string]*targetPublisherEntry), limit: targetPublisherLimit, now: time.Now}
	p.connect = func(ctx context.Context, cfg MQTTConfig) (mqtt.Client, error) {
		cfg.BeforeConnect, cfg.OnConnectCallback = nil, nil
		opts := newMQTTClientOptions(cfg, a.logger)
		opts.SetCleanSession(true)
		opts.SetResumeSubs(false)
		opts.SetAutoReconnect(false)
		opts.SetConnectionLostHandler(func(_ mqtt.Client, err error) {
			a.logger.WithError(err).Warn("Target publisher disconnected; next send requires fresh authorization")
		})
		client := mqtt.NewClient(opts)
		if err := WaitTokenContext(ctx, client.Connect(), connectAttemptTimeout); err != nil {
			client.Disconnect(0)
			return nil, err
		}
		return client, nil
	}
	a.targetPublishers = p
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				p.close()
				return
			case <-ticker.C:
				p.reap()
			}
		}
	}()
	return nil
}

func (p *targetPublisherPool) reap() {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	for id, e := range p.entries {
		if !e.busy && (now.Sub(e.used) >= targetPublisherIdle || now.Sub(e.created) >= targetPublisherLifetime) {
			e.client.Disconnect(0)
			delete(p.entries, id)
		}
	}
}

func (p *targetPublisherPool) close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	for id, e := range p.entries {
		e.client.Disconnect(0)
		delete(p.entries, id)
	}
}

// CloseTargetPublisher completes connection cleanup before the service reports stopped.
func (a *Adapter) CloseTargetPublisher() {
	if a.targetPublishers != nil {
		a.targetPublishers.close()
	}
}

func (p *targetPublisherPool) acquire(ctx context.Context, target string) (*targetPublisherEntry, error) {
	clientID, err := bc.TargetPublisherClientID(p.config.ClientID, target)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || p.ctx.Err() != nil || ctx.Err() != nil {
		return nil, errors.New("target publisher stopped")
	}
	now := p.now()
	if e := p.entries[target]; e != nil {
		if e.busy {
			return nil, errTargetPublisherBusy
		}
		if e.client.IsConnectionOpen() && now.Sub(e.created) < targetPublisherLifetime {
			e.busy = true
			return e, nil
		}
		e.client.Disconnect(0)
		delete(p.entries, target)
	}
	if len(p.entries) >= p.limit {
		oldestID := ""
		var oldest *targetPublisherEntry
		for id, e := range p.entries {
			if !e.busy && (oldest == nil || e.used.Before(oldest.used)) {
				oldestID, oldest = id, e
			}
		}
		if oldest == nil {
			return nil, errors.New("target publisher capacity exhausted")
		}
		oldest.client.Disconnect(0)
		delete(p.entries, oldestID)
	}
	cfg := p.config
	cfg.ClientID = clientID
	dialCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(p.ctx, cancel)
	defer stop()
	defer cancel()
	client, err := p.connect(dialCtx, cfg)
	if err != nil {
		return nil, err
	}
	if dialCtx.Err() != nil {
		client.Disconnect(0)
		return nil, dialCtx.Err()
	}
	e := &targetPublisherEntry{client: client, created: p.now(), used: p.now(), busy: true}
	p.entries[target] = e
	return e, nil
}

func (p *targetPublisherPool) publish(ctx context.Context, target, topic string, payload []byte) error {
	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var e *targetPublisherEntry
	var err error
	for {
		e, err = p.acquire(waitCtx, target)
		if !errors.Is(err, errTargetPublisherBusy) {
			break
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-waitCtx.Done():
			timer.Stop()
			return waitCtx.Err()
		case <-timer.C:
		}
	}
	if err != nil {
		return err
	}
	err = WaitTokenContext(ctx, e.client.Publish(topic, 1, false, payload), 5*time.Second)
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.entries[target] == e {
		if err != nil || p.closed {
			e.client.Disconnect(0)
			delete(p.entries, target)
		} else {
			e.busy = false
			e.used = p.now()
		}
	}
	return err
}

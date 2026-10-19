// Package mosquittodriver implements the bounded, asynchronous control plane of
// the community Mosquitto plugin. It never calls broker APIs from a Go worker.
package mosquittodriver

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	bc "project/internal/brokercontract"
)

type Config struct {
	OwnerURL, Token, Cell, Instance string
	MaxPending, MaxSessions         int
	Timeout                         time.Duration
}
type session struct {
	id                           uint64
	pointer                      uintptr
	request                      bc.AuthRequest
	grant                        bc.Grant
	ready, connected, refreshing bool
	result                       error
	refreshAt                    time.Time
	cancel                       context.CancelFunc
	eventSeq                     uint64
	lastObservation              time.Time
	closed                       bool
	retireAt                     time.Time
}
type Completion struct {
	ID      string
	Pointer uintptr
	Allow   bool
}
type Driver struct {
	mu         sync.Mutex
	cfg        Config
	http       *http.Client
	sessions   map[uintptr]*session
	pending    map[string]*session
	sequence   uint64
	activeAuth int
	bootID     string
	ctx        context.Context
	cancel     context.CancelFunc
}

func New(cfg Config) (*Driver, error) {
	u, err := url.Parse(cfg.OwnerURL)
	if err != nil || u.User != nil || u.Host == "" || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("invalid owner URL")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "localhost" || net.ParseIP(u.Hostname()).IsLoopback())) {
		return nil, errors.New("owner requires HTTPS or loopback HTTP")
	}
	if len(cfg.Token) < 32 || cfg.Cell == "" || cfg.Instance == "" {
		return nil, errors.New("owner service token, cell and instance required")
	}
	if cfg.MaxPending > 1024 || cfg.MaxSessions > 200000 {
		return nil, errors.New("authentication or session capacity exceeds supported bound")
	}
	if cfg.MaxPending <= 0 {
		cfg.MaxPending = 128
	}
	if cfg.MaxSessions <= 0 {
		cfg.MaxSessions = 65536
	}
	if cfg.Timeout <= 0 || cfg.Timeout > 2*time.Second {
		cfg.Timeout = 2 * time.Second
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Driver{cfg: cfg, http: &http.Client{Timeout: cfg.Timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, sessions: map[uintptr]*session{}, pending: map[string]*session{}, bootID: nonce(), ctx: ctx, cancel: cancel}, nil
}
func nonce() string {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b[:])
}

// Begin copies all credentials before returning to the C callback. A duplicate
// pending ClientID is rejected, so delayed completion cannot bind another socket.
func (d *Driver) Begin(ptr uintptr, wireID, user, password, peer string, protocol int) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.ctx.Err() != nil || wireID == "" || len(wireID) > 1024 || len(user) > 2048 || len(password) > 4096 || d.activeAuth >= d.cfg.MaxPending || len(d.pending) >= d.cfg.MaxPending || len(d.sessions) >= d.cfg.MaxSessions {
		return false
	}
	if _, ok := d.pending[wireID]; ok {
		return false
	}
	if old := d.sessions[ptr]; old != nil {
		old.cancel()
		if d.pending[old.request.WireClientID] == old {
			delete(d.pending, old.request.WireClientID)
		}
		delete(d.sessions, ptr)
	}
	d.sequence++
	d.activeAuth++
	ctx, cancel := context.WithCancel(d.ctx)
	s := &session{id: d.sequence, pointer: ptr, cancel: cancel, request: bc.AuthRequest{RequestID: nonce(), Provider: "mosquitto", BrokerCell: d.cfg.Cell, BrokerInstance: d.cfg.Instance, ConnectionNonce: nonce(), WireClientID: wireID, Username: user, Password: password, Peer: peer, Protocol: protocol}}
	d.sessions[ptr] = s
	d.pending[wireID] = s
	go d.authenticate(ctx, s, false)
	return true
}
func (d *Driver) authenticate(ctx context.Context, s *session, refresh bool) {
	body, err := json.Marshal(s.request)
	var grant bc.Grant
	if err == nil {
		var req *http.Request
		req, err = http.NewRequestWithContext(ctx, "POST", strings.TrimRight(d.cfg.OwnerURL, "/")+"/v1/authenticate", bytes.NewReader(body))
		if err == nil {
			req.Header.Set("Authorization", "Bearer "+d.cfg.Token)
			req.Header.Set("Content-Type", "application/json")
			var resp *http.Response
			resp, err = d.http.Do(req)
			if err == nil {
				defer resp.Body.Close()
				if resp.StatusCode != 200 {
					err = errors.New("owner rejected authentication")
				} else {
					dec := json.NewDecoder(io.LimitReader(resp.Body, 65537))
					dec.DisallowUnknownFields()
					err = dec.Decode(&grant)
					if err == nil {
						var extra any
						if dec.Decode(&extra) != io.EOF {
							err = errors.New("invalid authentication response")
						}
					}
				}
			}
		}
	}
	if err == nil && bc.ValidateGrant(grant) != nil {
		err = errors.New("invalid grant policy")
	}
	if err == nil && (!grant.Allow || grant.Principal.TenantID == "" || grant.Principal.DeviceID == "" || grant.ConnectionID == "" || grant.RouteEpoch == 0 || grant.PolicyVersion == "" || grant.MappingVersion == "" || time.Until(grant.ExpiresAt) <= 0 || time.Until(grant.ExpiresAt) > 300*time.Second) {
		err = errors.New("invalid or denied grant")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.activeAuth--
	if d.sessions[s.pointer] != s || s.closed || ctx.Err() != nil {
		return
	}
	if refresh && err == nil && (grant.ConnectionID != s.grant.ConnectionID || grant.Principal != s.grant.Principal || grant.RouteEpoch != s.grant.RouteEpoch) {
		err = errors.New("grant identity changed")
	}
	s.result = err
	s.refreshing = false
	s.ready = true
	s.refreshAt = time.Now().Add(5 * time.Second)
	if err == nil {
		s.grant = grant
	} else if refresh {
		s.grant.Allow = false
	}
}

// Poll runs only on the broker main thread. C passes the returned ClientID to
// mosquitto_complete_basic_auth without dereferencing any retained client pointer.
func (d *Driver) Poll() *Completion {
	d.mu.Lock()
	defer d.mu.Unlock()
	for id, s := range d.pending {
		if !s.ready {
			continue
		}
		delete(d.pending, id)
		ok := s.result == nil
		if !ok {
			s.cancel()
			delete(d.sessions, s.pointer)
		}
		return &Completion{ID: id, Pointer: s.pointer, Allow: ok}
	}
	return nil
}
func (d *Driver) Connected(ptr uintptr) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if s := d.sessions[ptr]; s != nil && s.ready && s.result == nil {
		s.connected = true
	}
}
func (d *Driver) Closed(ptr uintptr) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if s := d.sessions[ptr]; s != nil {
		s.cancel()
		if d.pending[s.request.WireClientID] == s {
			delete(d.pending, s.request.WireClientID)
		}
		if s.connected {
			s.closed = true
			s.connected = false
			s.retireAt = time.Now().Add(5 * time.Second)
			s.request.Password = ""
		} else {
			delete(d.sessions, ptr)
		}
	}
}
func (d *Driver) IsClosed(ptr uintptr) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	s := d.sessions[ptr]
	return s != nil && s.closed
}

// Tick refreshes a bounded subset asynchronously. ACL checks continue to enforce
// the existing lease; refresh failures immediately revoke it, including delivery.
func (d *Driver) Tick() {
	d.mu.Lock()
	defer d.mu.Unlock()
	n := 0
	for ptr, s := range d.sessions {
		if s.closed {
			if time.Now().After(s.retireAt) {
				delete(d.sessions, ptr)
			}
			continue
		}
		if s.ready && !s.connected {
			if _, pending := d.pending[s.request.WireClientID]; !pending && time.Now().After(s.refreshAt) {
				s.cancel()
				delete(d.sessions, ptr)
				continue
			}
		}
		if n >= d.cfg.MaxPending || d.activeAuth >= d.cfg.MaxPending {
			break
		}
		if s.connected && !s.refreshing && time.Now().After(s.refreshAt) {
			s.refreshing = true
			d.activeAuth++
			n++
			go d.authenticate(d.ctx, s, true)
		}
	}
}
func (d *Driver) snapshot(ptr uintptr) (bc.Grant, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	s := d.sessions[ptr]
	if s == nil || s.closed || s.result != nil || !s.grant.Allow || !time.Now().Before(s.grant.ExpiresAt) {
		return bc.Grant{}, errors.New("no active grant")
	}
	return s.grant, nil
}

// Authorize is called for WRITE, READ and SUBSCRIBE. READ checks every delivery,
// including old durable subscriptions, against the current lease.
func (d *Driver) Authorize(ptr uintptr, access int, topic string, size int, qos byte, retain bool) bool {
	g, err := d.snapshot(ptr)
	if err != nil {
		return false
	}
	maxSize := g.Policy.MaxPayload
	// The service receives a transport envelope containing base64 plus trusted
	// metadata. AuthorizePayload separately enforces the decoded business limit.
	if access == 1 && g.Principal.Kind == bc.PrincipalService && strings.HasPrefix(topic, bc.IngressPrefix+"/") {
		maxSize = bc.MaxEnvelopeBytes
	}
	if size < 0 || size > maxSize || qos > g.Policy.MaxQoS {
		return false
	}
	if access == 1 && bc.IsCommandTopic(topic) && g.BindingPhase == "connecting" {
		return false
	}
	action := bc.Action("publish")
	switch access {
	case 2:
	case 1:
		action = bc.Action("receive")
	case 4:
		action = bc.Action("subscribe")
	default:
		return false
	}
	if retain && g.Policy.RetainProfile == "live-ingress" && access == 2 {
		return false
	}
	return bc.AuthorizeTopic(g.Principal, g.Policy, action, topic) == nil
}

// Rewrite performs no I/O: identity and policy come from the immutable lease.
func (d *Driver) Rewrite(ptr uintptr, topic string, payload []byte, qos byte, retain bool) (string, []byte, bool, error) {
	g, e := d.snapshot(ptr)
	if e != nil {
		return "", nil, false, e
	}
	if string(g.Principal.Kind) == "service" {
		if !d.Authorize(ptr, 2, topic, len(payload), qos, retain) {
			return "", nil, false, errors.New("service publish denied")
		}
		if bc.IsCommandFrame(payload) {
			f, err := bc.ParseCommandFrame(payload, time.Now())
			if err != nil || f.WireTopic != topic || retain {
				return "", nil, false, errors.New("command storage frame denied")
			}
		} else if bc.IsCommandTopic(topic) {
			return "", nil, false, errors.New("command requires durable storage frame")
		}
		return topic, payload, retain, nil
	}
	if !d.Authorize(ptr, 2, topic, len(payload), qos, retain) {
		return "", nil, false, errors.New("publish denied")
	}
	if g.Policy.RetainProfile == "wire-retained" {
		return "", nil, false, errors.New("wire-retained ingress requires certified side channel")
	}
	t, e := bc.IngressTopic(g.Principal.TenantID, g.Principal.DeviceID, []byte(topic))
	if e != nil {
		return "", nil, false, e
	}
	if retain && len(payload) == 0 && g.Policy.RetainProfile == "platform-retained" {
		return t, []byte{}, true, nil
	}
	m := bc.Metadata{ContractVersion: "1", TenantID: g.Principal.TenantID, PrincipalKind: g.Principal.Kind, BrokerCell: d.cfg.Cell, BrokerInstance: d.cfg.Instance, ConnectionID: g.ConnectionID, RouteEpoch: g.RouteEpoch, PolicyVersion: g.PolicyVersion, MappingVersion: g.MappingVersion, OriginalQoS: qos, OriginalRetain: retain, ObservedAt: time.Now().UTC()}
	// EncodeEnvelope sets the original wire topic from this fixed transport value.
	m.OriginalTopicB64 = base64Topic(topic)
	b, e := bc.EncodeEnvelope(g.Principal, m, payload)
	return t, b, retain, e
}
func base64Topic(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }
func (d *Driver) Close() {
	d.cancel()
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, s := range d.sessions {
		s.cancel()
	}
	d.sessions = map[uintptr]*session{}
	d.pending = map[string]*session{}
}
func (d *Driver) Stats() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return fmt.Sprintf("sessions=%d pending=%d limit=%s", len(d.sessions), len(d.pending), strconv.Itoa(d.cfg.MaxSessions))
}

// Observation snapshots the connection-scoped principal on the broker thread.
// Online/offline use sequence 1/2, making late online unable to revive a tombstone.
func (d *Driver) Observation(ptr uintptr, online bool) (string, []byte, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	s := d.sessions[ptr]
	if s == nil || s.result != nil || !s.grant.Allow {
		return "", nil, errors.New("no connection grant")
	}
	return d.observationLocked(s, online)
}
func (d *Driver) observationLocked(s *session, online bool) (string, []byte, error) {
	g := s.grant
	if g.Principal.Kind == bc.PrincipalService {
		return "", nil, errors.New("service has no device state")
	}
	s.eventSeq++
	s.lastObservation = time.Now()
	return bc.EncodeState(bc.StateObservation{ContractVersion: "1", Principal: g.Principal, BrokerCell: d.cfg.Cell, BrokerInstance: d.cfg.Instance, NodeBootID: d.bootID, ConnectionID: g.ConnectionID, RouteEpoch: g.RouteEpoch, MappingVersion: g.MappingVersion, PolicyVersion: g.PolicyVersion, EventSeq: s.eventSeq, Online: online, ObservedAt: time.Now().UTC()})
}

// NextObservation is polled from the main broker thread. It emits at most one
// fresh online observation per active connection per 30 seconds, with no queue.
func (d *Driver) NextObservation() (string, []byte, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, s := range d.sessions {
		if s.connected && s.result == nil && s.grant.Allow && s.grant.Principal.Kind != bc.PrincipalService && time.Since(s.lastObservation) >= 30*time.Second {
			return d.observationLocked(s, true)
		}
	}
	return "", nil, nil
}

// RewriteWill is only used by the ACL_WRITE callback after the matching socket
// offline/disconnect callback, while Mosquitto is delivering that socket's Will.
// The original Will remains in the broker's native wire path. This authenticated
// side channel is the only business ingress and never guesses a current identity
// from a ClientID. Delayed/restored Wills without a valid bound grant are denied.
func (d *Driver) RewriteWill(ptr uintptr, topic string, payload []byte, qos byte, retain bool) (string, []byte, bool, error) {
	d.mu.Lock()
	s := d.sessions[ptr]
	if s == nil || !s.closed || s.result != nil || !s.grant.Allow || !time.Now().Before(s.grant.ExpiresAt) {
		d.mu.Unlock()
		return "", nil, false, errors.New("no bound will grant")
	}
	g := s.grant
	d.mu.Unlock()
	// A device Will uses the preserved native wire path. It must never inject
	// an actuator command or impersonate a private service storage frame.
	if g.Principal.Kind != bc.PrincipalService && (bc.IsCommandTopic(topic) || bc.IsCommandFrame(payload)) {
		return "", nil, false, errors.New("device will cannot originate command storage")
	}
	if len(payload) > g.Policy.MaxPayload || qos > g.Policy.MaxQoS || bc.AuthorizeTopic(g.Principal, g.Policy, bc.ActionPublish, topic) != nil {
		return "", nil, false, errors.New("will denied")
	}
	if retain && g.Policy.RetainProfile == bc.RetainedLiveIngress {
		return "", nil, false, errors.New("retained will not certified")
	}
	if g.Principal.Kind == bc.PrincipalService {
		return "", nil, retain, nil
	}
	target, e := bc.IngressTopic(g.Principal.TenantID, g.Principal.DeviceID, []byte(topic))
	if e != nil {
		return "", nil, false, e
	}
	if retain && len(payload) == 0 && g.Policy.RetainProfile == bc.RetainedPlatform {
		return target, []byte{}, retain, nil
	}
	b, e := bc.EncodeEnvelope(g.Principal, bc.Metadata{ContractVersion: "1", TenantID: g.Principal.TenantID, PrincipalKind: g.Principal.Kind, BrokerCell: d.cfg.Cell, BrokerInstance: d.cfg.Instance, ConnectionID: g.ConnectionID, RouteEpoch: g.RouteEpoch, PolicyVersion: g.PolicyVersion, MappingVersion: g.MappingVersion, OriginalTopicB64: bc.EncodeOriginalTopic([]byte(topic)), OriginalQoS: qos, OriginalRetain: retain, ObservedAt: time.Now().UTC()}, payload)
	return target, b, retain, e
}

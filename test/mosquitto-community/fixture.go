package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	bc "project/internal/brokercontract"
)

type fixture struct {
	token           string
	mu              sync.Mutex
	calls           map[string]int
	cancelled       map[string]int
	auths           []fixtureAuth
	barrierRelease  chan struct{}
	barrierReleased bool
	barrierWaiting  int
	barrierMax      int
	barrierCalls    int
}

type fixtureAuth struct {
	Username        string `json:"username"`
	WireClientID    string `json:"wire_client_id"`
	Protocol        int    `json:"protocol"`
	ConnectionNonce string `json:"connection_nonce"`
	ConnectionID    string `json:"connection_id"`
	ReceivedAt      string `json:"received_at"`
	ReturnedAt      string `json:"returned_at,omitempty"`
	Result          string `json:"result,omitempty"`
	DurationMS      int64  `json:"duration_ms,omitempty"`
}

func newFixture(token string) *fixture {
	return &fixture{token: token, calls: map[string]int{}, cancelled: map[string]int{}, barrierRelease: make(chan struct{})}
}

func (f *fixture) authenticate(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	if r.Header.Get("Authorization") != "Bearer "+f.token {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Header.Get("Content-Type") != "application/json" {
		http.Error(w, "content type required", http.StatusUnsupportedMediaType)
		return
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, 8193))
	dec.DisallowUnknownFields()
	var req bc.AuthRequest
	if err := dec.Decode(&req); err != nil || bc.ValidateAuthRequest(req) != nil {
		http.Error(w, "invalid auth request", http.StatusBadRequest)
		return
	}
	if dec.Decode(new(any)) != io.EOF {
		http.Error(w, "trailing auth data", http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	f.calls[req.Username]++
	callNumber := f.calls[req.Username]
	f.auths = append(f.auths, fixtureAuth{Username: req.Username, WireClientID: req.WireClientID, Protocol: req.Protocol, ConnectionNonce: req.ConnectionNonce, ConnectionID: fixtureConnectionID(req.ConnectionNonce), ReceivedAt: started.UTC().Format(time.RFC3339Nano)})
	authIndex := len(f.auths) - 1
	f.mu.Unlock()
	if strings.HasPrefix(req.Username, "qa-barrier-") {
		f.mu.Lock()
		f.barrierCalls++
		f.barrierWaiting++
		if f.barrierWaiting > f.barrierMax {
			f.barrierMax = f.barrierWaiting
		}
		release := f.barrierRelease
		f.mu.Unlock()
		select {
		case <-release:
		case <-r.Context().Done():
			f.mu.Lock()
			f.barrierWaiting--
			f.mu.Unlock()
			return
		}
		f.mu.Lock()
		f.barrierWaiting--
		f.mu.Unlock()
	}

	delay := time.Duration(0)
	switch {
	case req.Username == "qa-timeout":
		delay = 2300 * time.Millisecond
	case req.Username == "qa-slow-cancel":
		delay = 1200 * time.Millisecond
	case req.Username == "qa-slow-overdue" || req.Username == "qa-late-response":
		delay = 3500 * time.Millisecond
	}
	if delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		if req.Username == "qa-late-response" {
			<-timer.C
		} else {
			select {
			case <-timer.C:
			case <-r.Context().Done():
				f.mu.Lock()
				f.cancelled[req.Username]++
				f.mu.Unlock()
				return
			}
		}
	}
	if r.Context().Err() != nil && req.Username != "qa-late-response" {
		f.mu.Lock()
		f.cancelled[req.Username]++
		f.mu.Unlock()
		return
	}

	grant, err := fixedGrant(req, callNumber)
	if err != nil {
		f.finishAuth(authIndex, "fixture_rejected", started)
		http.Error(w, "fixture has no grant for requested identity", http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(grant)
	f.finishAuth(authIndex, "grant_returned", started)
}

func (f *fixture) finishAuth(index int, result string, started time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if index >= 0 && index < len(f.auths) {
		f.auths[index].ReturnedAt = time.Now().UTC().Format(time.RFC3339Nano)
		f.auths[index].Result = result
		f.auths[index].DurationMS = time.Since(started).Milliseconds()
	}
}

func fixedGrant(req bc.AuthRequest, callNumber int) (bc.Grant, error) {
	if req.Username == "qa-deny" || (req.Username == "qa-refresh-deny" && callNumber > 1) || (req.Username == "qa-will-revoked" && callNumber > 1) {
		return bc.Grant{Allow: false, DenyCode: "fixture-denied"}, nil
	}
	var principal bc.Principal
	policy := bc.Policy{MaxPayload: bc.MaxPayloadBytes, MaxQoS: 1, RetainProfile: bc.RetainedLiveIngress}
	switch {
	case req.Username == "qa-service" || req.Username == "qa-retained-service" || req.Username == "qa-will-service" || req.Username == "qa-shared-service" || req.Username == "qa-command-service":
		principal = bc.Principal{TenantID: "qa-tenant", DeviceID: "qa-service", Kind: bc.PrincipalService}
		policy.Subscribe = []string{bc.IngressPrefix + "/#", bc.StatePrefix + "/#"}
		policy.Publish = []string{"commands/device-a/#"}
		if req.Username == "qa-command-service" {
			policy.Publish = []string{"devices/command/device-a/+"}
		}
		if req.Username == "qa-shared-service" {
			policy.Subscribe = []string{"$share/qa-workers/" + bc.IngressPrefix + "/#"}
		}
		if req.Username == "qa-retained-service" {
			for _, item := range []struct{ device, wire string }{
				{"device-a", "telemetry/device-a/snapshot"},
				{"device-a", "telemetry/device-a/config"},
				{"device-b", "telemetry/device-b/snapshot"},
			} {
				topic, err := bc.IngressTopic("qa-tenant", item.device, []byte(item.wire))
				if err != nil {
					return bc.Grant{}, err
				}
				policy.Subscribe = append(policy.Subscribe, topic)
			}
		}
		if req.Username == "qa-will-service" {
			policy.Subscribe = []string{bc.IngressPrefix + "/#", bc.StatePrefix + "/#", "telemetry/device-a/#"}
		}
	case req.Username == "qa-refresh-deny":
		principal = bc.Principal{TenantID: "qa-tenant", DeviceID: "device-a", Kind: bc.PrincipalDevice}
		policy.Publish = []string{"telemetry/device-a/#"}
		policy.Subscribe = []string{"commands/device-a/#"}
	case req.Username == "qa-late-response":
		principal = bc.Principal{TenantID: "qa-tenant", DeviceID: "device-a", Kind: bc.PrincipalDevice}
		policy.Publish = []string{"telemetry/device-a/#"}
		policy.Subscribe = []string{"commands/device-a/#"}
	case strings.HasPrefix(req.Username, "qa-retained-device-"):
		deviceID := strings.TrimPrefix(req.Username, "qa-retained-device-")
		if deviceID == "" {
			return bc.Grant{}, errors.New("empty retained device id")
		}
		principal = bc.Principal{TenantID: "qa-tenant", DeviceID: deviceID, Kind: bc.PrincipalDevice}
		policy.Publish = []string{"telemetry/" + deviceID + "/#"}
		policy.Subscribe = []string{"commands/" + deviceID + "/#"}
		policy.RetainProfile = bc.RetainedPlatform
	case req.Username == "qa-will-expired" || req.Username == "qa-will-revoked":
		principal = bc.Principal{TenantID: "qa-tenant", DeviceID: "device-a", Kind: bc.PrincipalDevice}
		policy.Publish = []string{"telemetry/device-a/#"}
		policy.Subscribe = []string{"commands/device-a/#"}
	case req.Username == "qa-will-device":
		principal = bc.Principal{TenantID: "qa-tenant", DeviceID: "device-a", Kind: bc.PrincipalDevice}
		policy.Publish = []string{"telemetry/device-a/#"}
		policy.Subscribe = []string{"commands/device-a/#"}
	case req.Username == "qa-command-device-a" || req.Username == "qa-command-device-b" || req.Username == "qa-command-revoked-device-a":
		deviceID := strings.TrimPrefix(req.Username, "qa-command-")
		if deviceID == "revoked-device-a" {
			deviceID = "device-a"
		}
		principal = bc.Principal{TenantID: "qa-tenant", DeviceID: deviceID, Kind: bc.PrincipalDevice}
		policy.Publish = []string{"telemetry/" + deviceID + "/#"}
		if req.Username == "qa-command-revoked-device-a" {
			policy.Subscribe = []string{"commands/device-a/#"}
		} else if deviceID == "device-a" {
			policy.Subscribe = []string{"devices/command/device-a/+"}
		} else {
			policy.Subscribe = []string{"devices/command/device-b/+"}
		}
	case strings.HasPrefix(req.Username, "qa-device-"):
		deviceID := strings.TrimPrefix(req.Username, "qa-device-")
		if deviceID == "" {
			return bc.Grant{}, errors.New("empty device id")
		}
		principal = bc.Principal{TenantID: "qa-tenant", DeviceID: deviceID, Kind: bc.PrincipalDevice}
		policy.Publish = []string{"telemetry/" + deviceID + "/#"}
		policy.Subscribe = []string{"commands/" + deviceID + "/#"}
	case strings.HasPrefix(req.Username, "qa-slow") || strings.HasPrefix(req.Username, "qa-barrier-") || req.Username == "qa-timeout":
		principal = bc.Principal{TenantID: "qa-tenant", DeviceID: "device-a", Kind: bc.PrincipalDevice}
		policy.Publish = []string{"telemetry/device-a/#"}
		policy.Subscribe = []string{"commands/device-a/#"}
	default:
		return bc.Grant{}, fmt.Errorf("unknown test identity %q", req.Username)
	}
	if err := bc.ValidatePolicy(policy); err != nil {
		return bc.Grant{}, err
	}
	expires := time.Now().Add(45 * time.Second)
	if req.Username == "qa-will-expired" {
		expires = time.Now().Add(1200 * time.Millisecond)
	}
	return bc.Grant{
		Allow: true, Principal: principal, Policy: policy,
		PolicyVersion: "fixture-policy-v1", MappingVersion: "fixture-map-v1",
		ConnectionID: fixtureConnectionID(req.ConnectionNonce), RouteEpoch: 1,
		ExpiresAt: expires,
	}, nil
}

func fixtureConnectionID(nonce string) string { return "fixture-conn-" + nonce }

type fixtureStats struct {
	Calls          map[string]int `json:"calls"`
	Cancelled      map[string]int `json:"cancelled"`
	Auths          []fixtureAuth  `json:"auths"`
	BarrierCalls   int            `json:"barrier_calls"`
	BarrierWaiting int            `json:"barrier_waiting"`
	BarrierMax     int            `json:"barrier_max"`
}

func (f *fixture) stats(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(fixtureStats{Calls: cloneCounts(f.calls), Cancelled: cloneCounts(f.cancelled), Auths: append([]fixtureAuth(nil), f.auths...), BarrierCalls: f.barrierCalls, BarrierWaiting: f.barrierWaiting, BarrierMax: f.barrierMax})
}

func (f *fixture) releaseBarrier(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	if !f.barrierReleased {
		close(f.barrierRelease)
		f.barrierReleased = true
	}
	f.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

// requestBrokerRestart is a loopback-only QA control for persistence tests.
// The fixture has no broker signal handle; the container supervisor watches
// this marker and restarts only the isolated QA broker process.
func (f *fixture) requestBrokerRestart(w http.ResponseWriter, r *http.Request) {
	if err := os.WriteFile("/tmp/tp-mosquitto-qa.restart", []byte("restart\n"), 0o600); err != nil {
		http.Error(w, "restart request failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func cloneCounts(src map[string]int) map[string]int {
	dst := make(map[string]int, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

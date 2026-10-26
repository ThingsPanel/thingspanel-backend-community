package mosquittodriver

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	bc "project/internal/brokercontract"
)

// Runtime control never retains a C pointer or invokes a broker API. The HTTP
// worker only stages a bounded result; Tick installs it on the broker main
// thread. Its ACK is sent by a later request, after callbacks see the fence.
func (d *Driver) requestRuntime(acks []bc.RuntimeFenceAck) (bc.RuntimeSyncResponse, error) {
	var out bc.RuntimeSyncResponse
	body, e := json.Marshal(bc.RuntimeSyncRequest{BrokerCell: d.cfg.Cell, BrokerInstance: d.cfg.Instance, BootID: d.bootID, Applied: acks})
	if e != nil {
		return out, e
	}
	req, e := http.NewRequestWithContext(d.ctx, http.MethodPost, strings.TrimRight(d.cfg.OwnerURL, "/")+"/v1/runtime/sync", bytes.NewReader(body))
	if e != nil {
		return out, e
	}
	req.Header.Set("Authorization", "Bearer "+d.cfg.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, e := d.http.Do(req)
	if e != nil {
		return out, e
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return out, errors.New("runtime control unavailable")
	}
	raw, e := io.ReadAll(io.LimitReader(resp.Body, (128<<10)+1))
	if e != nil || len(raw) > 128<<10 || bc.ValidateUniqueJSON(raw) != nil {
		return out, errors.New("invalid runtime control response")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if e := dec.Decode(&out); e != nil {
		return out, errors.New("invalid runtime control response")
	}
	if out.Overflow || len(out.Fences) > bc.MaxRuntimeFences {
		return out, errors.New("runtime fence capacity exceeded")
	}
	for _, f := range out.Fences {
		if f.ChangeID == "" || len(f.ChangeID) > 36 || f.DeniedEpoch == 0 || bc.ValidatePrincipal(bc.Principal{TenantID: f.TenantID, DeviceID: f.DeviceID, Kind: bc.PrincipalDevice}) != nil {
			return out, errors.New("invalid runtime fence")
		}
	}
	return out, nil
}
func runtimeScope(tenant, device string) string { return tenant + "\x00" + device }
func (d *Driver) runtimeGrantAllowedLocked(g bc.Grant) bool {
	return !d.cfg.RuntimeFencing || (time.Now().Before(d.runtimeValidUntil) && g.RouteEpoch > d.runtimeFences[runtimeScope(g.Principal.TenantID, g.Principal.DeviceID)])
}
func (d *Driver) installRuntimeLocked(response bc.RuntimeSyncResponse) {
	fences := make(map[string]uint64, len(response.Fences))
	acks := make([]bc.RuntimeFenceAck, 0, len(response.Fences))
	previous := map[string]bool{}
	for _, a := range d.runtimeAcks {
		previous[a.ChangeID] = true
	}
	newAck := false
	for _, f := range response.Fences {
		key := runtimeScope(f.TenantID, f.DeviceID)
		if f.DeniedEpoch > fences[key] {
			fences[key] = f.DeniedEpoch
		}
		if !f.NeedsAck {
			continue
		}
		proof, _ := json.Marshal(struct {
			Version, Cell, Instance, Boot, Change, Tenant, Device string
			Epoch                                                 uint64
		}{"mosquitto-main-thread-fence-v1", d.cfg.Cell, d.cfg.Instance, d.bootID, f.ChangeID, f.TenantID, f.DeviceID, f.DeniedEpoch})
		sum := sha256.Sum256(proof)
		acks = append(acks, bc.RuntimeFenceAck{RuntimeFence: f, EvidenceSHA256: hex.EncodeToString(sum[:])})
		if !previous[f.ChangeID] {
			newAck = true
		}
	}
	d.runtimeFences, d.runtimeAcks = fences, acks
	d.runtimeValidUntil = time.Now().Add(10 * time.Second)
	if newAck {
		d.runtimeNext = time.Time{}
	}
}
func (d *Driver) tickRuntimeLocked() {
	if !d.cfg.RuntimeFencing {
		return
	}
	if d.runtimePending != nil {
		d.installRuntimeLocked(*d.runtimePending)
		// A stalled main loop cannot turn an old HTTP snapshot into a fresh
		// control lease. The deadline begins at request initiation.
		d.runtimeValidUntil = d.runtimePendingDeadline
		d.runtimePending = nil
	}
	if d.runtimeSyncing || time.Now().Before(d.runtimeNext) || d.ctx.Err() != nil {
		return
	}
	d.runtimeSyncing = true
	d.runtimeNext = time.Now().Add(5 * time.Second)
	acks := append([]bc.RuntimeFenceAck(nil), d.runtimeAcks...)
	go func() {
		started := time.Now()
		response, e := d.requestRuntime(acks)
		d.mu.Lock()
		defer d.mu.Unlock()
		d.runtimeSyncing = false
		if e == nil && d.ctx.Err() == nil {
			d.runtimePending = &response
			d.runtimePendingDeadline = started.Add(10 * time.Second)
		}
	}()
}

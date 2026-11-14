package mosquittodriver

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// Called only after the local grant has been denied. This acknowledgement does
// not depend on TCP disconnect: WRITE, READ and SUBSCRIBE already fail closed.
func (d *Driver) startCredentialAckLocked(s *session) {
	if d.activeCredentialAcks >= d.cfg.MaxPending || s.credentialAckInFlight || s.credentialAck.ConnectionID == "" || s.credentialAck.ExpiresAt.IsZero() {
		return
	}
	s.credentialAckInFlight = true
	d.activeCredentialAcks++
	s.credentialAckNext = time.Now().Add(5 * time.Second)
	ack := s.credentialAck
	go func() {
		raw, _ := json.Marshal(ack)
		req, err := http.NewRequestWithContext(d.ctx, http.MethodPost, strings.TrimRight(d.cfg.OwnerURL, "/")+"/v1/credential-leases/ack", bytes.NewReader(raw))
		ok := false
		if err == nil {
			req.Header.Set("Authorization", "Bearer "+d.cfg.Token)
			req.Header.Set("Content-Type", "application/json")
			if resp, e := d.http.Do(req); e == nil {
				ok = resp.StatusCode == http.StatusNoContent
				resp.Body.Close()
			}
		}
		d.mu.Lock()
		defer d.mu.Unlock()
		s.credentialAckInFlight = false
		d.activeCredentialAcks--
		if ok && s.credentialAck.ConnectionID == ack.ConnectionID && s.credentialAck.ExpiresAt.Equal(ack.ExpiresAt) {
			s.credentialAckPending = false
		}
	}()
}

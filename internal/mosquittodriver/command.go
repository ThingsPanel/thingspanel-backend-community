package mosquittodriver

import (
	"errors"
	bc "project/internal/brokercontract"
	"strings"
	"time"
)

// AuthorizePayload adds delivery-time freshness and identity checks. These run
// for every queued/retransmitted delivery, including broker restart recovery.
func (d *Driver) AuthorizePayload(ptr uintptr, access int, topic string, payload []byte, qos byte, retain bool) bool {
	// Mosquitto invokes READ both to insert into an offline session queue and
	// to deliver. A valid service-originated storage frame may enter a bounded
	// offline queue without a live socket lease. MESSAGE_OUT below remains the
	// mandatory actual-delivery gate and never uses this admission exception.
	if access == 1 && bc.IsCommandFrame(payload) {
		d.mu.Lock()
		s := d.sessions[ptr]
		offline := s == nil || s.closed
		d.mu.Unlock()
		if offline {
			f, err := bc.ParseCommandFrame(payload, time.Now())
			return err == nil && f.WireTopic == topic && qos <= 1 && !retain
		}
	}
	if !d.Authorize(ptr, access, topic, len(payload), qos, retain) {
		return false
	}
	if access != 1 {
		return true
	}
	if g, err := d.snapshot(ptr); err == nil && g.Principal.Kind == bc.PrincipalService && strings.HasPrefix(topic, bc.IngressPrefix+"/") {
		if len(payload) == 0 {
			return true
		} // platform-retained slot clear, no business data
		env, err := bc.ParseIngress(topic, payload, bc.TrustedMetadata{BrokerCell: d.cfg.Cell, BrokerInstance: d.cfg.Instance})
		return err == nil && len(env.Values) <= g.Policy.MaxPayload
	}
	if !bc.IsCommandFrame(payload) {
		return !bc.IsCommandTopic(topic)
	}
	g, err := d.snapshot(ptr)
	if err != nil {
		return false
	}
	if g.Principal.Kind == bc.PrincipalService {
		return false
	}
	f, err := bc.ParseCommandFrame(payload, time.Now())
	return err == nil && !retain && f.WireTopic == topic && f.TenantID == g.Principal.TenantID && f.TargetDeviceID == g.Principal.DeviceID && f.RouteEpoch == g.RouteEpoch && f.MappingVersion == g.MappingVersion
}

// Outgoing copies only payload bytes. Stored messages keep the private frame so
// an offline/retry delivery cannot lose its expiry and fencing metadata.
func (d *Driver) Outgoing(ptr uintptr, topic string, payload []byte, qos byte, retain bool) ([]byte, bool, error) {
	if !bc.IsCommandFrame(payload) {
		if bc.IsCommandTopic(topic) {
			return nil, false, errors.New("unframed command delivery denied")
		}
		return nil, false, nil
	}
	// Never use the offline queue admission exception for network output.
	if !d.Authorize(ptr, 1, topic, len(payload), qos, retain) {
		return nil, false, errors.New("command delivery denied")
	}
	f, err := bc.ParseCommandFrame(payload, time.Now())
	if err != nil {
		return nil, false, err
	}
	g, err := d.snapshot(ptr)
	if err != nil || g.Principal.Kind == bc.PrincipalService || retain || f.WireTopic != topic || f.TenantID != g.Principal.TenantID || f.TargetDeviceID != g.Principal.DeviceID || f.RouteEpoch != g.RouteEpoch || f.MappingVersion != g.MappingVersion {
		return nil, false, errors.New("command delivery identity or route denied")
	}
	return f.Payload, true, err
}

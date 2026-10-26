package mqttadapter

import (
	"context"
	"errors"
	bc "project/internal/brokercontract"
	"project/internal/brokerledger"
)

type ingressRejector interface {
	Reject(context.Context, brokerledger.Rejection) error
}

func permanentAcceptanceReason(err error) string {
	switch {
	case errors.Is(err, brokerledger.ErrRouteRetired):
		return "retired_transport_route"
	case errors.Is(err, brokerledger.ErrConflict):
		return "source_event_id_conflict"
	case errors.Is(err, brokerledger.ErrTooLarge):
		return "business_message_exceeds_limit"
	case errors.Is(err, brokerledger.ErrInvalid):
		return "invalid_business_message"
	default:
		return ""
	}
}

// Only invoke after ParseIngress has authenticated the full envelope. Permanent
// business rejection is ACKed only after durable quarantine commits. Dependency
// failures continue to backpressure; no application success response is sent.
func (a *Adapter) rejectTrustedIngress(topic string, body []byte, env bc.Envelope, reason string, cause error) error {
	r, ok := a.ingressAcceptor.(ingressRejector)
	if !ok {
		return errors.Join(cause, errors.New("durable ingress quarantine unavailable"))
	}
	err := r.Reject(a.adapterContext(), brokerledger.Rejection{TenantID: env.Metadata.TenantID, DeviceID: env.DeviceID, BrokerCell: env.Metadata.BrokerCell, RouteEpoch: env.Metadata.RouteEpoch, Topic: topic, Body: body, Reason: reason})
	if err != nil {
		return errors.Join(cause, err)
	}
	a.logger.WithFields(map[string]interface{}{"device_id": env.DeviceID, "tenant_id": env.Metadata.TenantID, "reason": reason}).Warn("trusted ingress permanently rejected and durably quarantined; no business success")
	return nil
}

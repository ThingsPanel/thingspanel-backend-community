package service

import (
	"context"
	"errors"

	"project/internal/brokerstate"
	"project/pkg/global"
	"project/pkg/utils"
)

var ErrBrokerEvidenceUnauthorized = errors.New("broker evidence requires tenant identity")
var ErrBrokerEvidenceUnsupported = errors.New("broker evidence requires ingress_v1")

// BrokerStatusEvidence accepts only the authenticated tenant. Tenant selectors
// in query strings or request bodies never override it, including for admins.
func (*Device) BrokerStatusEvidence(ctx context.Context, claims *utils.UserClaims, deviceID string) (brokerstate.Evidence, error) {
	if claims == nil || claims.ID == "" || claims.TenantID == "" {
		return brokerstate.Evidence{}, ErrBrokerEvidenceUnauthorized
	}
	if !isIngressV1Commands() {
		return brokerstate.Evidence{}, ErrBrokerEvidenceUnsupported
	}
	return brokerstate.New(global.DB).InspectCurrent(ctx, claims.TenantID, deviceID)
}

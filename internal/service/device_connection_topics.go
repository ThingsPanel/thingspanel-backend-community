package service

import (
	"context"
	"project/internal/brokercontrol"
	"project/pkg/global"
	"project/pkg/utils"
)

func (*Device) ConnectionTopics(ctx context.Context, claims *utils.UserClaims, id string) (brokercontrol.ConnectionTopics, error) {
	if claims == nil || claims.ID == "" || claims.TenantID == "" {
		return brokercontrol.ConnectionTopics{}, ErrBrokerEvidenceUnauthorized
	}
	if !isIngressV1Commands() {
		return brokercontrol.ConnectionTopics{}, ErrBrokerEvidenceUnsupported
	}
	return brokercontrol.NewGormRepository(global.DB).ReadConnectionTopics(ctx, claims.TenantID, id)
}

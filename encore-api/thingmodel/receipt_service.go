package thingmodel

import (
	"context"

	"encore.app/thingmodel/app"
)

type ReceiptQueryRequest struct {
	ExpectedPayloadHash string `query:"expectedPayloadHash"`
}

// QueryCommandReceipt reads this caller's original command outcome. The tenant
// and subject always come from verified auth, including when an administrator
// queries; neither can be selected from the request. Querying does not mutate or
// retry the business command.
//
//encore:api auth method=GET path=/api/v2/thing-model-commands/receipts/:operation/:key
func (s *Service) QueryCommandReceipt(ctx context.Context, operation, key string, req *ReceiptQueryRequest) (*app.ReceiptObservation, error) {
	principal, err := currentPrincipal()
	if err != nil {
		return nil, err
	}
	expectedHash := ""
	if req != nil {
		expectedHash = req.ExpectedPayloadHash
	}
	observation, err := app.ReadReceipt(ctx, postgresRepository{}, principal, operation, key, expectedHash)
	if err != nil {
		return nil, apiError(err)
	}
	return &observation, nil
}

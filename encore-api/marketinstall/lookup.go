package marketinstall

import (
	"context"

	"encore.app/marketinstall/app"
	"encore.dev/beta/errs"
)

type OperationLookupRequest struct {
	OperationKey string `query:"operationKey"`
}

// GetByKey recovers the original operation after the initial POST response is
// lost. Encore authenticates the current session; the canonical management
// permission is the same as Install/Get/Resume. Original execution grants are
// neither renewed nor required for a read of the original subject's receipt.
//
//encore:api auth method=GET path=/api/v2/market-installation-operations
func (s *Service) GetByKey(ctx context.Context, req *OperationLookupRequest) (*app.OperationLookup, error) {
	identity, err := installationIdentity()
	if err != nil {
		return nil, err
	}
	if req == nil {
		return nil, apiError(app.ErrInvalid)
	}
	if s.ledger == nil {
		return nil, &errs.Error{Code: errs.Unavailable, Message: "installation query is unavailable"}
	}
	return lookupAuthorizedOperation(ctx, s.ledger, identity, req.OperationKey, requireCurrentInstallationManagement)
}

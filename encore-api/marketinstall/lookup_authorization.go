package marketinstall

import (
	"context"
	"errors"
	"os"

	"encore.app/marketinstall/app"
	"encore.dev/beta/errs"
)

const installationReadResource = "api/v1/device/market/bundles/install"

// authorizeInstallationManagement delegates only current management-permission
// evaluation. The native auth handler has already verified the live session.
// This does not issue/renew an execution grant. No user JWT, execution grant,
// caller-selected resource or operation plan is transmitted.
func (p *httpProvider) authorizeInstallationManagement(ctx context.Context, identity app.Identity) error {
	if p == nil || len(p.token) < 32 || identity.TenantID == "" || identity.SubjectID == "" {
		return &errs.Error{Code: errs.Unavailable, Message: "installation management authorization is unavailable"}
	}
	request := struct {
		ProtocolVersion int    `json:"protocolVersion"`
		TenantID        string `json:"tenantId"`
		SubjectID       string `json:"subjectId"`
	}{1, identity.TenantID, identity.SubjectID}
	var response struct {
		ProtocolVersion int    `json:"protocolVersion"`
		Authorized      bool   `json:"authorized"`
		TenantID        string `json:"tenantId"`
		SubjectID       string `json:"subjectId"`
		Resource        string `json:"resource"`
	}
	if err := p.call(ctx, "read-authorize", request, &response); err != nil {
		var status *providerStatusError
		if errors.As(err, &status) && (status.Status == 401 || status.Status == 403) {
			return &errs.Error{Code: errs.PermissionDenied, Message: "current installation resource permission required"}
		}
		return &errs.Error{Code: errs.Unavailable, Message: "installation management authorization could not be confirmed"}
	}
	if response.ProtocolVersion != 1 || !response.Authorized || response.TenantID != identity.TenantID || response.SubjectID != identity.SubjectID || response.Resource != installationReadResource {
		return &errs.Error{Code: errs.PermissionDenied, Message: "current installation resource permission required"}
	}
	return nil
}

func lookupAuthorizedOperation(ctx context.Context, ledger app.OperationKeyReader, identity app.Identity, key string, authorize func(context.Context, app.Identity) error) (*app.OperationLookup, error) {
	if authorize == nil {
		return nil, &errs.Error{Code: errs.Unavailable, Message: "installation read authorization is unavailable"}
	}
	if err := authorize(ctx, identity); err != nil {
		return nil, err
	}
	observation, err := app.LookupByKey(ctx, ledger, identity, key)
	if err != nil {
		return nil, apiError(err)
	}
	return &observation, nil
}

func configuredInstallationManagementProvider() (*httpProvider, error) {
	provider, err := newHTTPProvider(os.Getenv("TP_MARKETINSTALL_PROVIDER_BASE_URL"), os.Getenv("TP_MARKETINSTALL_PROVIDER_TOKEN"))
	if err != nil {
		return nil, &errs.Error{Code: errs.Unavailable, Message: "installation management authorization is unavailable"}
	}
	return provider, nil
}

// Every public installation-management entrypoint checks this same canonical
// resource before reading or changing the ledger. Execution still requires its
// original scoped grant and the coordinator/provider state constraints.
func requireCurrentInstallationManagement(ctx context.Context, identity app.Identity) error {
	provider, err := configuredInstallationManagementProvider()
	if err != nil {
		return err
	}
	return provider.authorizeInstallationManagement(ctx, identity)
}

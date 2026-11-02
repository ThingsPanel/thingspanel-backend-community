package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/go-basic/uuid"
	"project/internal/dal"
	"project/internal/model"
	"project/internal/repo"
	"project/pkg/global"
	"project/pkg/utils"
)

func marketInstallDigest(value interface{}) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var valueObject interface{}
	if err := decoder.Decode(&valueObject); err != nil {
		return "", err
	}
	canonical, err := json.Marshal(valueObject)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}
func marketInstallStepHash(step model.MarketInstallStep) (string, error) {
	return marketInstallDigest(struct {
		Version int
		Step    model.MarketInstallStep
	}{1, step})
}
func marketInstallStepKey(operationID string, step model.MarketInstallStep) string {
	hash, _ := marketInstallDigest(struct {
		Version                        int
		Operation, Step, Owner, Action string
	}{1, operationID, step.ID, step.Owner, step.Action})
	return hash
}
func marketInstallRequestDigest(req model.MarketInstallRequest, tenant, subject string) (string, error) {
	if req.OverwritePolicy == "" {
		req.OverwritePolicy = "default"
	}
	req.DeviceBindings = append([]model.DeviceBindingInput{}, req.DeviceBindings...)
	sort.Slice(req.DeviceBindings, func(i, j int) bool { return req.DeviceBindings[i].BindingKey < req.DeviceBindings[j].BindingKey })
	return marketInstallDigest(struct {
		Version         int
		Tenant, Subject string
		Request         model.MarketInstallRequest
	}{1, tenant, subject, req})
}

// IssueMarketInstallAuthorization runs only in the initially authenticated
// request. It stores a scoped immutable plan and the existing session marker,
// never a user JWT or market credential.
func IssueMarketInstallAuthorization(ctx context.Context, req model.MarketInstallRequest, plan model.MarketInstallPlan, claims *utils.UserClaims, rawJWT, key string) (*repo.MarketInstallAuthorization, error) {
	if claims == nil || claims.ID == "" || claims.TenantID == "" || rawJWT == "" || global.REDIS == nil {
		return nil, repo.ErrMarketInstallUnauthorized
	}
	// Plan preparation may have made remote calls since the initial API check.
	// Recheck the same current permission before creating a session marker or
	// immutable execution grant. This assertion does not replace the account,
	// auth-version, delegation and original-session checks below.
	if err := requireCurrentMarketInstallationManagement(ctx, claims); err != nil {
		return nil, err
	}
	expiry := time.Now().UTC().Add(time.Hour)
	tokenExpiry := time.Unix(claims.ExpiresAt, 0)
	if tokenExpiry.Before(expiry) {
		expiry = tokenExpiry
	}
	if !expiry.After(time.Now()) {
		return nil, repo.ErrMarketInstallUnauthorized
	}
	requestDigest, err := marketInstallRequestDigest(req, claims.TenantID, claims.ID)
	if err != nil {
		return nil, err
	}
	planDigest, err := marketInstallDigest(plan)
	if err != nil {
		return nil, err
	}
	rawPlan, err := json.Marshal(plan)
	if err != nil {
		return nil, err
	}
	grant := repo.MarketInstallAuthorization{ID: uuid.New(), RequestKey: key, TenantID: claims.TenantID, SubjectID: claims.ID, Authority: claims.Authority, AuthVersion: claims.AuthVersion, ActorID: claims.ActorID, ActorVersion: claims.ActorVersion, Delegated: claims.Delegated, Purpose: "a08.install.v1", RequestDigest: requestDigest, PlanDigest: planDigest, Plan: rawPlan, SessionHash: dal.AppPushSessionKey(rawJWT), ExpiresAt: expiry, CreatedAt: time.Now().UTC()}
	if err := repo.MarketInstallAccountCurrent(ctx, grant); err != nil {
		return nil, err
	}
	// Reuse the identity owner's derived marker and logout/single-session cleanup.
	// No marker is created if the originally authenticated session disappeared.
	ok, err := global.REDIS.Eval(ctx, `if redis.call('GET',KEYS[1])~='1' then return 0 end;local ttl=redis.call('PTTL',KEYS[1]);local cap=tonumber(ARGV[1]);if ttl<0 or ttl>cap then ttl=cap end;if ttl<=0 then return 0 end;redis.call('SET',KEYS[2],'1','PX',ttl);return 1`, []string{rawJWT, grant.SessionHash}, time.Until(expiry).Milliseconds()).Int()
	if err != nil {
		return nil, err
	}
	if ok != 1 {
		return nil, repo.ErrMarketInstallUnauthorized
	}
	if err := repo.CreateMarketInstallAuthorization(ctx, &grant); err != nil {
		return nil, err
	}
	return &grant, nil
}
func readCurrentMarketInstallGrant(ctx context.Context, identity model.MarketInstallIdentity) (*repo.MarketInstallAuthorization, model.MarketInstallPlan, error) {
	if global.REDIS == nil {
		return nil, model.MarketInstallPlan{}, repo.ErrMarketInstallUnauthorized
	}
	grant, err := repo.ReadMarketInstallAuthorization(ctx, identity.AuthorizationRef, identity.TenantID, identity.SubjectID)
	if err != nil {
		return nil, model.MarketInstallPlan{}, err
	}
	marker, err := global.REDIS.Get(ctx, grant.SessionHash).Result()
	if err != nil {
		return nil, model.MarketInstallPlan{}, err
	}
	if marker != "1" {
		return nil, model.MarketInstallPlan{}, repo.ErrMarketInstallUnauthorized
	}
	var plan model.MarketInstallPlan
	if err := json.Unmarshal(grant.Plan, &plan); err != nil {
		return nil, plan, err
	}
	digest, err := marketInstallDigest(plan)
	if err != nil || digest != grant.PlanDigest {
		return nil, plan, errors.New("installation plan integrity failure")
	}
	// A still-live immutable grant does not retain management permission after
	// the current policy is withdrawn. Recheck the existing permission owner on
	// every provider entry, before operation binding or business dispatch. This
	// is an in-process policy check, not a receiver transaction/epoch fence.
	if _, err := AuthorizeCurrentMarketInstallationManagement(ctx, model.MarketInstallationReadIdentity{ProtocolVersion: 1, TenantID: grant.TenantID, SubjectID: grant.SubjectID}); err != nil {
		return nil, model.MarketInstallPlan{}, err
	}
	return grant, plan, nil
}
func ReadMarketInstallPreparedPlan(ctx context.Context, req model.MarketInstallPrepareRequest) (model.MarketInstallPlan, error) {
	grant, plan, err := readCurrentMarketInstallGrant(ctx, req.MarketInstallIdentity)
	if err != nil {
		return plan, err
	}
	digest, err := marketInstallRequestDigest(req.Request, req.TenantID, req.SubjectID)
	if err != nil {
		return plan, err
	}
	if req.ProtocolVersion != 1 || digest != grant.RequestDigest {
		return model.MarketInstallPlan{}, repo.ErrMarketInstallUnauthorized
	}
	return plan, nil
}
func AuthorizeMarketInstallStep(ctx context.Context, req model.MarketInstallStepRequest) (model.MarketInstallPlan, error) {
	_, plan, err := readCurrentMarketInstallGrant(ctx, req.MarketInstallIdentity)
	if err != nil {
		return plan, err
	}
	if req.ProtocolVersion != 1 || req.Generation < 1 || req.OperationID == "" || len(req.OperationID) > 128 {
		return plan, repo.ErrMarketInstallUnauthorized
	}
	for index, step := range plan.Steps {
		if step.ID != req.StepID {
			continue
		}
		expectedHash, err := marketInstallStepHash(step)
		if err != nil {
			return plan, err
		}
		actualHash, err := marketInstallStepHash(req.Step)
		if err != nil {
			return plan, err
		}
		if actualHash != expectedHash || req.PayloadHash != expectedHash || req.IdempotencyKey != marketInstallStepKey(req.OperationID, step) || len(req.PriorResults) != index {
			return plan, repo.ErrMarketInstallUnauthorized
		}
		for i, prior := range req.PriorResults {
			hash, err := marketInstallStepHash(plan.Steps[i])
			if err != nil {
				return plan, err
			}
			if prior.StepID != plan.Steps[i].ID || prior.Receipt.Owner != plan.Steps[i].Owner || prior.Receipt.PayloadHash != hash || prior.Receipt.ResourceID == "" || !json.Valid(prior.Receipt.Data) {
				return plan, repo.ErrMarketInstallUnauthorized
			}
		}
		if err := repo.BindMarketInstallGrantOperation(ctx, req.AuthorizationRef, req.TenantID, req.SubjectID, req.OperationID); err != nil {
			return plan, err
		}
		return plan, nil
	}
	return plan, repo.ErrMarketInstallUnauthorized
}

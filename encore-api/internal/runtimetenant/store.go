// Package runtimetenant owns reviewed, immutable Community source bindings.
// UUID normalization is a consistency check, never evidence of authorization.
package runtimetenant

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"strings"

	"encore.app/internal/tenantidentity"
	"github.com/google/uuid"
)

var ErrUnproven = errors.New("runtime tenant source is not proven")

type Scope struct {
	Environment       string `json:"environment"`
	ProviderID        string `json:"providerId"`
	Namespace         string `json:"namespace"`
	ServiceInstanceID string `json:"serviceInstanceId"`
	CommunityOrigin   string `json:"communityOrigin"`
}
type Binding struct {
	ID          string `json:"id"`
	TenantID    string `json:"nativeTenantId"`
	RawTenantID string `json:"sourceTenantId"`
	Generation  int64  `json:"generation"`
	Scope       Scope  `json:"scope"`
}

type Principal struct {
	UserID, RawTenantID, Authority string
	Delegated                      bool
}
type Verifier interface {
	Verify(context.Context, string) (Principal, error)
}

type RowScanner interface{ Scan(...any) error }
type Queryer interface {
	QueryRowContext(context.Context, string, ...any) RowScanner
}
type SQLReader struct{ DB *sql.DB }

func (r SQLReader) QueryRowContext(ctx context.Context, q string, args ...any) RowScanner {
	return r.DB.QueryRowContext(ctx, q, args...)
}

func (s Scope) Validate() error {
	for _, v := range []string{s.Environment, s.ProviderID, s.Namespace} {
		if v == "" || v != strings.TrimSpace(v) || len(v) > 128 {
			return ErrUnproven
		}
	}
	if len(s.ServiceInstanceID) > 128 || s.ServiceInstanceID != strings.TrimSpace(s.ServiceInstanceID) {
		return ErrUnproven
	}
	u, e := url.Parse(s.CommunityOrigin)
	if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") || s.CommunityOrigin != strings.TrimRight(s.CommunityOrigin, "/") {
		return ErrUnproven
	}
	return nil
}

func Lookup(ctx context.Context, q Queryer, tenantID string, scope Scope) (Binding, error) {
	var b Binding
	b.Scope = scope
	parsed, e := uuid.Parse(tenantID)
	if e != nil || parsed == uuid.Nil || parsed.String() != tenantID || scope.Validate() != nil {
		return b, ErrUnproven
	}
	e = q.QueryRowContext(ctx, `SELECT id::text,tenant_id::text,source_tenant_id,generation FROM runtime_tenant_sources WHERE tenant_id=$1 AND environment=$2 AND provider_id=$3 AND namespace=$4 AND service_instance_id=$5 AND community_origin=$6 AND state='active' FOR SHARE`, tenantID, scope.Environment, scope.ProviderID, scope.Namespace, scope.ServiceInstanceID, scope.CommunityOrigin).Scan(&b.ID, &b.TenantID, &b.RawTenantID, &b.Generation)
	if e != nil || b.RawTenantID == "" || tenantidentity.NormalizePrincipalID(b.RawTenantID) != tenantID {
		return Binding{}, ErrUnproven
	}
	return b, nil
}

// Provision is not an HTTP or tenant-self-service operation. Its verifier must
// check two independent live identities. Historical data is never modified.
func Provision(ctx context.Context, db *sql.DB, verifier Verifier, adminToken, tenantToken string, scope Scope, reviewedTenant, manifestDigest string) (Binding, error) {
	if db == nil || verifier == nil || scope.Validate() != nil || !digestValid(manifestDigest) {
		return Binding{}, ErrUnproven
	}
	admin, e := verifier.Verify(ctx, adminToken)
	if e != nil || admin.Delegated || admin.Authority != "SYS_ADMIN" || admin.RawTenantID != "" || admin.UserID == "" {
		return Binding{}, ErrUnproven
	}
	tenant, e := verifier.Verify(ctx, tenantToken)
	if e != nil || tenant.Delegated || tenant.Authority != "TENANT_ADMIN" || tenant.RawTenantID == "" || tenant.RawTenantID != strings.TrimSpace(tenant.RawTenantID) || tenant.UserID == "" || tenant.UserID == admin.UserID {
		return Binding{}, ErrUnproven
	}
	native := tenantidentity.NormalizePrincipalID(tenant.RawTenantID)
	if native == uuid.Nil.String() || native != reviewedTenant {
		return Binding{}, ErrUnproven
	}
	b := Binding{ID: uuid.NewString(), TenantID: native, RawTenantID: tenant.RawTenantID, Generation: 1, Scope: scope}
	tx, e := db.BeginTx(ctx, nil)
	if e != nil {
		return Binding{}, ErrUnproven
	}
	defer tx.Rollback()
	_, e = tx.ExecContext(ctx, `INSERT INTO runtime_tenant_sources(id,tenant_id,source_tenant_id,environment,provider_id,namespace,service_instance_id,community_origin,generation,reviewed_manifest_sha256,platform_actor_id,tenant_actor_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,1,$9,$10,$11)`, b.ID, b.TenantID, b.RawTenantID, scope.Environment, scope.ProviderID, scope.Namespace, scope.ServiceInstanceID, scope.CommunityOrigin, manifestDigest, admin.UserID, tenant.UserID)
	if e != nil {
		return Binding{}, ErrUnproven
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO runtime_tenant_source_audit(id,binding_id,action,platform_actor_id,tenant_actor_id,reviewed_manifest_sha256) VALUES($1,$2,'provision',$3,$4,$5)`, uuid.NewString(), b.ID, admin.UserID, tenant.UserID, manifestDigest); e != nil {
		return Binding{}, ErrUnproven
	}
	if e = tx.Commit(); e != nil {
		return Binding{}, ErrUnproven
	}
	return b, nil
}

func digestValid(v string) bool {
	if len(v) != 64 {
		return false
	}
	for _, c := range v {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func Revoke(ctx context.Context, db *sql.DB, verifier Verifier, adminToken, tenantToken string, scope Scope, reviewedTenant, manifestDigest string) (Binding, error) {
	if db == nil || verifier == nil || scope.Validate() != nil || !digestValid(manifestDigest) {
		return Binding{}, ErrUnproven
	}
	admin, e := verifier.Verify(ctx, adminToken)
	if e != nil || admin.Delegated || admin.Authority != "SYS_ADMIN" || admin.RawTenantID != "" || admin.UserID == "" {
		return Binding{}, ErrUnproven
	}
	tenant, e := verifier.Verify(ctx, tenantToken)
	if e != nil || tenant.Delegated || tenant.Authority != "TENANT_ADMIN" || tenant.RawTenantID == "" || tenant.UserID == "" || tenant.UserID == admin.UserID || tenantidentity.NormalizePrincipalID(tenant.RawTenantID) != reviewedTenant {
		return Binding{}, ErrUnproven
	}
	tx, e := db.BeginTx(ctx, nil)
	if e != nil {
		return Binding{}, ErrUnproven
	}
	defer tx.Rollback()
	b := Binding{TenantID: reviewedTenant, RawTenantID: tenant.RawTenantID, Scope: scope}
	e = tx.QueryRowContext(ctx, `SELECT id::text,generation FROM runtime_tenant_sources WHERE tenant_id=$1 AND source_tenant_id=$2 AND environment=$3 AND provider_id=$4 AND namespace=$5 AND service_instance_id=$6 AND community_origin=$7 AND state='active' FOR UPDATE`, reviewedTenant, tenant.RawTenantID, scope.Environment, scope.ProviderID, scope.Namespace, scope.ServiceInstanceID, scope.CommunityOrigin).Scan(&b.ID, &b.Generation)
	if e != nil {
		return Binding{}, ErrUnproven
	}
	if _, e = tx.ExecContext(ctx, `UPDATE runtime_tenant_sources SET state='revoked',revoked_at=now() WHERE id=$1 AND state='active'`, b.ID); e != nil {
		return Binding{}, ErrUnproven
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO runtime_tenant_source_audit(id,binding_id,action,platform_actor_id,tenant_actor_id,reviewed_manifest_sha256) VALUES($1,$2,'revoke',$3,$4,$5)`, uuid.NewString(), b.ID, admin.UserID, tenant.UserID, manifestDigest); e != nil {
		return Binding{}, ErrUnproven
	}
	if e = tx.Commit(); e != nil {
		return Binding{}, ErrUnproven
	}
	return b, nil
}

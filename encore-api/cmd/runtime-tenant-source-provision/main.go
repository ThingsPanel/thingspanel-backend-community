// Local operator authority, following notification-source-provision. This tool
// has no HTTP route and never accepts identity claims supplied beside a token.
package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"encore.app/internal/authcore"
	"encore.app/internal/runtimetenant"
	"encore.app/l1-access/authn"
	_ "github.com/jackc/pgx/v5/stdlib"
)

type manifest struct {
	Scope             runtimetenant.Scope `json:"scope"`
	NativeTenantID    string              `json:"nativeTenantId"`
	DatabaseAuthority string              `json:"databaseAuthority"`
	DatabaseName      string              `json:"databaseName"`
	ReviewReference   string              `json:"reviewReference"`
}
type authFile struct {
	AdminToken  string `json:"adminToken"`
	TenantToken string `json:"tenantToken"`
}
type liveVerifier struct{ origin string }

func (v liveVerifier) Verify(ctx context.Context, token string) (runtimetenant.Principal, error) {
	var p runtimetenant.Principal
	if token == "" || token != strings.TrimSpace(token) || strings.ContainsAny(token, "\r\n\x00") {
		return p, runtimetenant.ErrUnproven
	}
	mode := strings.TrimSpace(os.Getenv("TP_AUTH_MODE"))
	if mode == "native" {
		runtime, e := authcore.Load()
		if e != nil {
			return p, runtimetenant.ErrUnproven
		}
		user, e := runtime.Core.Check(ctx, token)
		if e != nil {
			return p, runtimetenant.ErrUnproven
		}
		return runtimetenant.Principal{UserID: user.ID, RawTenantID: user.TenantID, Authority: user.Authority, Delegated: user.Delegated}, nil
	}
	if mode != "" && mode != "legacy-jwt" {
		return p, runtimetenant.ErrUnproven
	}
	if os.Getenv("TP_JWT_KEY") == "" || strings.TrimRight(strings.TrimSpace(os.Getenv("TP_COMMUNITY_SESSION_BASE_URL")), "/") != v.origin {
		return p, runtimetenant.ErrUnproven
	}
	// AuthHandler verifies the signature and current Community user/session. Do
	// not replace this with JSON decoding or a caller-supplied claims document.
	_, data, e := authn.AuthHandler(ctx, &authn.AuthParams{LegacyToken: token})
	if e != nil || data == nil || len(data.Roles) != 1 {
		return p, runtimetenant.ErrUnproven
	}
	// The already-verified signed token also has to be a direct session. The
	// public auth Data shape intentionally does not carry the delegation flag.
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return p, runtimetenant.ErrUnproven
	}
	payload, e := base64.RawURLEncoding.DecodeString(parts[1])
	if e != nil {
		return p, runtimetenant.ErrUnproven
	}
	var claims struct {
		Delegated bool   `json:"delegated"`
		ActorID   string `json:"actor_id"`
	}
	if json.Unmarshal(payload, &claims) != nil {
		return p, runtimetenant.ErrUnproven
	}
	return runtimetenant.Principal{UserID: data.UserID, RawTenantID: data.TenantID, Authority: strings.ToUpper(data.Roles[0]), Delegated: claims.Delegated || claims.ActorID != ""}, nil
}

func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e.Error())
		os.Exit(1)
	}
	fmt.Println("runtime tenant source provisioned; private receipt saved")
}
func run() error {
	manifestPath := flag.String("manifest", "", "absolute private reviewed scope manifest")
	authPath := flag.String("auth-file", "", "absolute private file with two actual current session tokens")
	output := flag.String("output", "", "new absolute private receipt path")
	flag.Parse()
	if flag.NArg() != 0 || *manifestPath == "" || *authPath == "" || *output == "" || !filepath.IsAbs(*output) {
		return errors.New("invalid operator arguments")
	}
	var m manifest
	raw, e := readPrivateJSON(*manifestPath, &m)
	if e != nil {
		return e
	}
	if m.Scope.Validate() != nil || m.ReviewReference == "" || len(m.ReviewReference) > 256 || m.DatabaseName == "" || m.DatabaseAuthority == "" {
		return errors.New("invalid reviewed manifest")
	}
	expected := runtimetenant.Scope{Environment: os.Getenv("TP_RUNTIME_TENANT_BINDING_ENVIRONMENT"), ProviderID: os.Getenv("TP_RUNTIME_COMMUNITY_PROVIDER_ID"), Namespace: os.Getenv("TP_RUNTIME_COMMUNITY_NAMESPACE"), ServiceInstanceID: os.Getenv("TP_RUNTIME_COMMUNITY_SERVICE_INSTANCE_ID"), CommunityOrigin: strings.TrimRight(strings.TrimSpace(os.Getenv("TP_COMMUNITY_BASE_URL")), "/")}
	if expected.Namespace == "" {
		expected.Namespace = "community"
	}
	if expected.Validate() != nil || m.Scope != expected {
		return errors.New("reviewed manifest does not match this deployment")
	}
	var credentials authFile
	if _, e = readPrivateJSON(*authPath, &credentials); e != nil {
		return e
	}
	dsn := os.Getenv("TP_RUNTIME_BINDING_DATABASE_DSN")
	u, e := url.Parse(dsn)
	if e != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Host == "" || u.Host+u.EscapedPath() != m.DatabaseAuthority || strings.TrimPrefix(u.Path, "/") != m.DatabaseName {
		return errors.New("database does not match reviewed target")
	}
	database, e := sql.Open("pgx", dsn)
	if e != nil {
		return errors.New("native binding database unavailable")
	}
	defer database.Close()
	database.SetMaxOpenConns(2)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var name, schema string
	if e = database.QueryRowContext(ctx, "SELECT current_database(),current_schema()").Scan(&name, &schema); e != nil || name != m.DatabaseName || schema != "public" {
		return errors.New("native binding database target mismatch")
	}
	sum := sha256.Sum256(raw)
	result, e := runtimetenant.Provision(ctx, database, liveVerifier{origin: m.Scope.CommunityOrigin}, credentials.AdminToken, credentials.TenantToken, m.Scope, m.NativeTenantID, hex.EncodeToString(sum[:]))
	if e != nil {
		return errors.New("runtime tenant source rejected; inspect target for an existing or unknown-commit binding before retry")
	}
	// The receipt contains no session token, API key or database credential.
	file, e := os.OpenFile(*output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return errors.New("binding committed but private receipt unavailable; reconcile before retry")
	}
	if e = json.NewEncoder(file).Encode(result); e != nil {
		file.Close()
		return errors.New("binding committed but private receipt incomplete; reconcile before retry")
	}
	if e = file.Close(); e != nil {
		return errors.New("binding committed but receipt close failed; reconcile before retry")
	}
	return nil
}
func readPrivateJSON(path string, target any) ([]byte, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("operator files must use absolute paths")
	}
	info, e := os.Lstat(path)
	if e != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("operator file must be a private regular file")
	}
	file, e := os.Open(path)
	if e != nil {
		return nil, errors.New("operator file unavailable")
	}
	defer file.Close()
	raw, e := io.ReadAll(io.LimitReader(file, 64<<10+1))
	if e != nil || len(raw) == 0 || len(raw) > 64<<10 {
		return nil, errors.New("operator file size invalid")
	}
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.DisallowUnknownFields()
	if d.Decode(target) != nil {
		return nil, errors.New("operator JSON invalid")
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return nil, errors.New("operator JSON has trailing data")
	}
	return raw, nil
}

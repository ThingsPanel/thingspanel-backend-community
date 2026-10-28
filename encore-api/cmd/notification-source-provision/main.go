package main

import (
	"context"
	"database/sql"
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

	"encore.app/l3-core/notification"
	"encore.app/l3-core/notification/contract"
	_ "github.com/jackc/pgx/v5/stdlib"
)

const maxAuthFileBytes = 64 << 10

type authFile struct {
	Admin  authToken `json:"admin"`
	Tenant authToken `json:"tenant"`
}

type authToken struct {
	Token  string          `json:"token"`
	Claims json.RawMessage `json:"claims"`
}

type infraConfig struct {
	SQLServers []struct {
		Host      string `json:"host"`
		TLSConfig struct {
			Disabled bool `json:"disabled"`
		} `json:"tls_config"`
		Databases map[string]struct {
			Name     string `json:"name"`
			Username string `json:"username"`
			Password string `json:"password"`
		} `json:"databases"`
	} `json:"sql_servers"`
}

func main() {
	if err := run(); err != nil {
		// Never print parse, database, authentication, or credential values.
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
	fmt.Fprintln(os.Stdout, "notification source/projection credentials provisioned; private output file written")
}

func run() error {
	authPath := flag.String("auth-file", "", "private JSON file containing current admin and tenant JWTs")
	outputPath := flag.String("output", "", "new private JSON output path; it must not already exist")
	ttl := flag.Duration("ttl", 2*time.Hour, "credential lifetime (maximum 8h)")
	renewPath := flag.String("renew-file", "", "private existing credential pair to renew; requires both current sessions")
	flag.Parse()
	if *authPath == "" || *outputPath == "" || flag.NArg() != 0 || *ttl <= 0 || *ttl > 8*time.Hour {
		return errors.New("invalid arguments")
	}
	if !filepath.IsAbs(*authPath) || !filepath.IsAbs(*outputPath) {
		return errors.New("paths must be absolute")
	}
	if err := requirePrivateRegularFile(*authPath); err != nil {
		return err
	}
	input, err := readAuthFile(*authPath)
	if err != nil {
		return err
	}
	infraPath := strings.TrimSpace(os.Getenv("ENCORE_INFRA_CONFIG_PATH"))
	mirrorPath := strings.TrimSpace(os.Getenv("NOTIFICATION_ENCORE_INFRA_CONFIG_PATH"))
	if infraPath == "" || infraPath != mirrorPath || !filepath.IsAbs(infraPath) {
		return errors.New("invalid infra path")
	}
	db, err := openNotificationDB(infraPath)
	if err != nil {
		return err
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return errors.New("notification database unavailable")
	}
	var credentialsTable sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT to_regclass('public.notification_credentials')::text`).Scan(&credentialsTable); err != nil || !credentialsTable.Valid {
		return errors.New("notification schema is not migrated")
	}
	store, err := notification.NewStore(notification.StoreOptions{DB: db})
	if err != nil {
		return errors.New("notification store unavailable")
	}
	privacy, err := notification.NewPrivacyGuardFromEnv()
	if err != nil {
		return errors.New("notification privacy configuration unavailable")
	}
	protector, err := notification.NewAESGCMProtectorFromEnv()
	if err != nil {
		return errors.New("notification keyring unavailable")
	}
	fixture := strings.TrimSpace(os.Getenv("NOTIFICATION_FIXTURE_MODE")) == "1"
	base := strings.TrimSpace(os.Getenv("NOTIFICATION_LEGACY_BASE_URL"))
	readerToken := strings.TrimSpace(os.Getenv("NOTIFICATION_LEGACY_READER_TOKEN"))
	deployment := strings.TrimSpace(os.Getenv("NOTIFICATION_SOURCE_DEPLOYMENT_ID"))
	sessions, err := notification.NewUserSessionChecker(base, notification.LegacySessionOptions{TestMode: fixture, AllowHTTPTestEndpoint: fixture, Timeout: 3 * time.Second})
	if err != nil {
		return errors.New("legacy session endpoint unavailable")
	}
	directory, err := notification.NewLegacyDirectoryReader(base, readerToken, deployment, notification.LegacySessionOptions{TestMode: fixture, AllowHTTPTestEndpoint: fixture, Timeout: 3 * time.Second})
	if err != nil {
		return errors.New("legacy directory endpoint unavailable")
	}
	management, err := notification.NewManagementService(store, store, store, store, store, sessions, provisionPluginClient{}, protector, privacy,
		notification.ManagementOptions{TestMode: fixture, AllowHTTPTestOrigin: fixture, TenantDirectory: directory})
	if err != nil {
		return errors.New("notification management unavailable")
	}
	var result notification.ProvisionedSourceCredentials
	if *renewPath != "" {
		if !filepath.IsAbs(*renewPath) || requirePrivateRegularFile(*renewPath) != nil {
			return errors.New("invalid renewal file")
		}
		f, openErr := os.Open(*renewPath)
		if openErr != nil {
			return errors.New("renewal file unavailable")
		}
		raw, readErr := io.ReadAll(io.LimitReader(f, maxAuthFileBytes+1))
		f.Close()
		var previous notification.ProvisionedSourceCredentials
		if readErr != nil || len(raw) > maxAuthFileBytes || json.Unmarshal(raw, &previous) != nil || previous.DeploymentID != deployment {
			return errors.New("invalid renewal file")
		}
		result, err = management.RenewSourceProjectionCredentials(ctx, input.Admin.Token, input.Tenant.Token, previous, *ttl)
	} else {
		result, err = management.ProvisionSourceProjectionCredentials(ctx, input.Admin.Token, input.Tenant.Token, deployment, *ttl)
	}
	if err != nil {
		return errors.New(safeProvisionFailure(err))
	}
	if err := writePrivateResult(*outputPath, result); err != nil {
		// The credential IDs are internal-only and the output contains the only
		// copy of the opaque values. Revoke both if delivery to the private file
		// failed, so the database cannot retain inaccessible credentials.
		if *renewPath == "" {
			_ = store.RevokeSourceProjectionCredentialPair(context.Background(), result.SourceCredentialID, result.ProjectionCredentialID)
		}
		return errors.New("private output could not be written")
	}
	return nil
}

func safeProvisionFailure(err error) string {
	switch {
	case errors.Is(err, notification.ErrPrivacyGuardClosed):
		return "notification privacy guard is closed"
	case errors.Is(err, notification.ErrIdentityUnauthorized):
		return "an identity token or live session was rejected"
	case errors.Is(err, notification.ErrIdentityForbidden), errors.Is(err, notification.ErrTenantContextRequired):
		return "platform or tenant role scope was rejected"
	case errors.Is(err, notification.ErrLegacyDirectoryUnavailable):
		return "legacy tenant directory is unavailable"
	case errors.Is(err, notification.ErrManagementNotFound):
		return "the authenticated tenant was not found in the legacy directory"
	case errors.Is(err, notification.ErrCredentialInvalid):
		return "credential scope or lifetime is invalid"
	case errors.Is(err, notification.ErrManagementInvalid):
		return "credential store rejected the scoped record"
	case errors.Is(err, notification.ErrManagementConflict):
		return "an active source/projection credential pair already exists for this scope"
	case errors.Is(err, notification.ErrManagementUnavailable):
		return "credential storage is unavailable"
	default:
		return "credential provisioning failed closed"
	}
}

type provisionPluginClient struct{}

func (provisionPluginClient) FetchManifest(context.Context, string, string) ([]byte, contract.PluginManifestV1, error) {
	return nil, contract.PluginManifestV1{}, errors.New("network operations disabled in credential provisioner")
}

func (provisionPluginClient) ValidateInstance(context.Context, notification.PluginRuntime, contract.PluginValidateRequestV1) (contract.PluginValidateResultV1, error) {
	return contract.PluginValidateResultV1{}, errors.New("network operations disabled in credential provisioner")
}

func readAuthFile(path string) (authFile, error) {
	var result authFile
	file, err := os.Open(path)
	if err != nil {
		return result, errors.New("auth file unavailable")
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxAuthFileBytes+1))
	if err != nil || len(data) == 0 || len(data) > maxAuthFileBytes {
		return result, errors.New("auth file invalid")
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil || strings.TrimSpace(result.Admin.Token) == "" || strings.TrimSpace(result.Tenant.Token) == "" {
		return authFile{}, errors.New("auth file invalid")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return authFile{}, errors.New("auth file invalid")
	}
	return result, nil
}

func requirePrivateRegularFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return errors.New("auth file must be a private regular file")
	}
	return nil
}

func openNotificationDB(infraPath string) (*sql.DB, error) {
	data, err := os.ReadFile(infraPath)
	if err != nil || len(data) == 0 || len(data) > 4<<20 {
		return nil, errors.New("infra config unavailable")
	}
	var cfg infraConfig
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	if decoder.Decode(&cfg) != nil || len(cfg.SQLServers) == 0 {
		return nil, errors.New("infra config invalid")
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return nil, errors.New("infra config invalid")
	}
	expectedDB := strings.TrimSpace(os.Getenv("NOTIFICATION_PROVISION_EXPECTED_DB"))
	if expectedDB == "" {
		expectedDB = "notification_local_ui"
	}
	for _, server := range cfg.SQLServers {
		physical, ok := server.Databases[expectedDB]
		if !ok || physical.Name != "notification" || physical.Username == "" || physical.Password == "" || !server.TLSConfig.Disabled || !localPostgresHost(server.Host) {
			continue
		}
		parsed := url.URL{Scheme: "postgres", Host: server.Host, Path: "/" + expectedDB, User: url.UserPassword(physical.Username, physical.Password)}
		query := parsed.Query()
		if server.TLSConfig.Disabled {
			query.Set("sslmode", "disable")
		}
		parsed.RawQuery = query.Encode()
		db, err := sql.Open("pgx", parsed.String())
		if err != nil {
			return nil, errors.New("notification database config invalid")
		}
		db.SetMaxOpenConns(2)
		db.SetMaxIdleConns(1)
		db.SetConnMaxLifetime(2 * time.Minute)
		return db, nil
	}
	return nil, errors.New("approved notification database mapping not found")
}

func localPostgresHost(host string) bool {
	parsed, err := url.Parse("tcp://" + host)
	if err != nil || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return false
	}
	expectedHost := strings.TrimSpace(os.Getenv("NOTIFICATION_PROVISION_EXPECTED_HOST"))
	if expectedHost == "" {
		expectedHost = "host.docker.internal:25543"
	}
	return parsed.Hostname() != "" && parsed.Port() != "" && host == expectedHost
}

func writePrivateResult(path string, value notification.ProvisionedSourceCredentials) error {
	dir := filepath.Dir(path)
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return errors.New("output directory must be private")
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return errors.New("private result encoding failed")
	}
	tmp := path + ".tmp"
	if _, err := os.Lstat(path); err == nil || !errors.Is(err, os.ErrNotExist) {
		return errors.New("private result path already exists or cannot be checked")
	}
	file, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("private result path unavailable")
	}
	ok := false
	defer func() {
		_ = file.Close()
		if !ok {
			_ = os.Remove(tmp)
		}
	}()
	if _, err := file.Write(data); err != nil {
		return errors.New("private result write failed")
	}
	if err := file.Sync(); err != nil {
		return errors.New("private result sync failed")
	}
	if err := file.Close(); err != nil {
		return errors.New("private result close failed")
	}
	if err := os.Link(tmp, path); err != nil {
		return errors.New("private result publish failed")
	}
	if err := os.Remove(tmp); err != nil {
		_ = os.Remove(path)
		return errors.New("private result cleanup failed")
	}
	ok = true
	return nil
}

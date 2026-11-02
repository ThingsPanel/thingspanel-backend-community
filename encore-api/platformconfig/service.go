// Package platformconfig is the sole native dictionary host. Its application
// and SQL implementations are injected and contain no Gin/global dependencies.
package platformconfig

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"encore.app/l1-access/authn"
	"encore.app/platformconfig/app"
	"encore.app/platformconfig/authorization"
	"encore.app/platformconfig/legacyresponse"
	"encore.app/platformconfig/store"
	"encore.app/platformconfig/transport"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

//encore:service
type Service struct {
	handler    *transport.Handler
	repository *store.Store
	database   *sql.DB
}

func initService() (*Service, error) {
	owner := os.Getenv("TP_PLATFORMCONFIG_OWNER")
	if owner == "" || owner == "disabled" {
		return &Service{}, nil
	}
	if owner != "encore" {
		return nil, fmt.Errorf("invalid dictionary owner selection")
	}
	mode := strings.TrimSpace(os.Getenv("TP_AUTH_MODE"))
	if mode != "native" && mode != "legacy-jwt" {
		return nil, fmt.Errorf("dictionary host requires an explicit production authentication mode")
	}
	config, err := readConfig(os.Getenv("TP_PLATFORMCONFIG_CONFIG_FILE"))
	if err != nil {
		return nil, err
	}
	permission, err := authorization.New(config.AuthorizationBaseURL, os.Getenv("TP_PLATFORMCONFIG_AUTHORIZATION_TOKEN"))
	if err != nil {
		return nil, fmt.Errorf("dictionary current-permission owner is not configured")
	}
	messages, err := legacyresponse.Load(config.MessagesFile)
	if err != nil {
		return nil, err
	}
	connection, err := pgx.ParseConfig(config.DatabaseURL)
	if err != nil || connection.Database != config.ExpectedDatabase || connection.Host == "" || connection.User == "" {
		return nil, fmt.Errorf("dictionary connection does not identify the configured existing database")
	}
	// Every connection targets the exact adopted table family, regardless of
	// a DSN option or session default. There is no fallback to another schema.
	if connection.RuntimeParams == nil {
		connection.RuntimeParams = map[string]string{}
	}
	connection.RuntimeParams["search_path"] = "public"
	connection.RuntimeParams["application_name"] = "thingspanel-platformconfig"
	connection.ConnectTimeout = 5 * time.Second
	database := stdlib.OpenDB(*connection)
	database.SetMaxOpenConns(8)
	database.SetMaxIdleConns(4)
	database.SetConnMaxLifetime(5 * time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var actual string
	if err := database.QueryRowContext(ctx, `SELECT current_database()`).Scan(&actual); err != nil || actual != config.ExpectedDatabase {
		_ = database.Close()
		return nil, fmt.Errorf("dictionary existing database could not be confirmed")
	}
	repository := store.New(database)
	if err := repository.ValidateReady(ctx); err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("dictionary schema adoption is not ready; run and verify its existing owner migration")
	}
	application := app.New(repository, permission, uuid.NewString, time.Now)
	return &Service{handler: transport.New(application, requestPrincipal, messages.Message), repository: repository, database: database}, nil
}

func requestPrincipal(_ *http.Request) (app.Principal, error) {
	return principalForData(authn.CurrentData())
}

func principalForData(data *authn.Data) (app.Principal, error) {
	if data == nil {
		return app.Principal{}, app.ErrUnauthorized
	}
	principal := app.Principal{SubjectID: data.UserID, TenantID: data.TenantID, Roles: append([]string(nil), data.Roles...)}
	if !app.CompleteIdentity(principal) {
		return app.Principal{}, app.ErrUnauthorized
	}
	return principal, nil
}

func (s *Service) serve(operation app.Operation, w http.ResponseWriter, r *http.Request) {
	if s == nil {
		var handler *transport.Handler
		handler.Serve(operation, w, r)
		return
	}
	s.handler.Serve(operation, w, r)
}

//encore:api auth raw sensitive method=POST path=/api/v1/dict/column
func (s *Service) CreateColumn(w http.ResponseWriter, r *http.Request) {
	s.serve(app.CreateColumn, w, r)
}

//encore:api auth raw sensitive method=POST path=/api/v1/dict/language
func (s *Service) CreateLanguage(w http.ResponseWriter, r *http.Request) {
	s.serve(app.CreateLanguage, w, r)
}

//encore:api auth raw sensitive method=GET path=/api/v1/dict/enum
func (s *Service) Enum(w http.ResponseWriter, r *http.Request) { s.serve(app.ReadEnum, w, r) }

//encore:api auth raw sensitive method=GET path=/api/v1/dict
func (s *Service) Page(w http.ResponseWriter, r *http.Request) { s.serve(app.ReadPage, w, r) }

//encore:api auth raw sensitive method=GET path=/api/v1/dict/language/:id
func (s *Service) Languages(w http.ResponseWriter, r *http.Request) { s.serve(app.ReadLanguages, w, r) }

//encore:api auth raw sensitive method=DELETE path=/api/v1/dict/column/:id
func (s *Service) DeleteColumn(w http.ResponseWriter, r *http.Request) {
	s.serve(app.DeleteColumn, w, r)
}

//encore:api auth raw sensitive method=DELETE path=/api/v1/dict/language/:id
func (s *Service) DeleteLanguage(w http.ResponseWriter, r *http.Request) {
	s.serve(app.DeleteLanguage, w, r)
}

//encore:api auth raw sensitive method=GET path=/api/v1/dict/protocol/service
func (s *Service) Protocols(w http.ResponseWriter, r *http.Request) { s.serve(app.ReadProtocols, w, r) }

// Readiness reveals no configuration, credentials, identities or table data.
// Disabled means unavailable, and does not establish a database connection.
//
//encore:api public raw method=GET path=/api/v1/platformconfig/health
func (s *Service) Health(w http.ResponseWriter, r *http.Request) {
	ready := false
	if s != nil && s.repository != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		ready = s.repository.ValidateReady(ctx) == nil
		cancel()
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if !ready {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	_ = json.NewEncoder(w).Encode(struct {
		Owner string `json:"owner"`
		Ready bool   `json:"ready"`
	}{Owner: "platformconfig", Ready: ready})
}

func (s *Service) Shutdown(_ context.Context) {
	if s != nil && s.database != nil {
		_ = s.database.Close()
	}
}

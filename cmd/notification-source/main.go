// notification-source is a local operator utility. It does not start the
// community API, producers, or the source outbox relay.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"project/initialize"
	"project/internal/app"
	"project/internal/dal"
	"project/internal/query"
	"project/internal/service"
	"project/pkg/global"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const usage = `Usage:
  notification-source [--config path] email-import preview --tenant-id ID --legacy-group-id ID --name NAME --smtp-instance-id ID
  notification-source [--config path] email-import apply --tenant-id ID --legacy-group-id ID --notification-group-id ID --group-revision N --expected-route-version N --idempotency-key KEY
  notification-source [--config path] route rollback --deployment-id ID --tenant-id ID --legacy-group-id ID --expected-route-version N

Preview prints a draft disabled native group body. Create and publish it through
the authorized Encore management flow before running apply. Apply verifies the
exact published snapshot before it registers the projection and switches the
source route. Rollback changes only future source events for the exact route
version; it does not recall or replay old outbox events.
`

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	rootFlags := flag.NewFlagSet("notification-source", flag.ContinueOnError)
	rootFlags.SetOutput(stderr)
	configPath := rootFlags.String("config", "", "database config file")
	if err := rootFlags.Parse(args); err != nil {
		return errors.New("invalid command arguments")
	}
	command := rootFlags.Args()
	if len(command) < 2 {
		_, _ = io.WriteString(stderr, usage)
		return errors.New("notification-source command is required")
	}

	switch command[0] {
	case "email-import":
		if command[1] == "preview" {
			return runEmailImportPreview(*configPath, command[2:], stdout, stderr)
		}
		if command[1] == "apply" {
			return runEmailImportApply(*configPath, command[2:], stdout, stderr)
		}
	case "route":
		if command[1] == "rollback" {
			return runRouteRollback(*configPath, command[2:], stdout, stderr)
		}
	}
	_, _ = io.WriteString(stderr, usage)
	return errors.New("unsupported notification-source command")
}

func runEmailImportPreview(configPath string, args []string, stdout, stderr io.Writer) error {
	flags := commandFlags("email-import preview", stderr)
	tenantID := flags.String("tenant-id", "", "legacy group tenant ID")
	legacyGroupID := flags.String("legacy-group-id", "", "legacy notification group ID")
	name := flags.String("name", "", "name for the native draft")
	targetID := flags.String("smtp-instance-id", "", "expected tenant-owned SMTP instance ID")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || *tenantID == "" || *legacyGroupID == "" || *name == "" || *targetID == "" {
		return errors.New("email import preview arguments incomplete")
	}
	_, closeDB, err := openOperatorDB(configPath)
	if err != nil {
		return err
	}
	defer closeDB()
	group, err := dal.GetNotificationGroupByTenantID(*legacyGroupID, *tenantID)
	if err != nil || group == nil {
		return errors.New("legacy notification group unavailable")
	}
	plan, err := service.PlanLegacyEmailGroupImportDraft(group, *tenantID, *name, *targetID)
	if err != nil {
		return errors.New("legacy email group cannot be planned")
	}
	response := struct {
		Status             string                        `json:"status"`
		TargetVerification string                        `json:"targetVerification"`
		TenantID           string                        `json:"tenantId"`
		LegacyGroupID      string                        `json:"legacyGroupId"`
		Plan               service.LegacyEmailImportPlan `json:"plan"`
		ApplyRequirement   string                        `json:"applyRequirement"`
	}{
		Status:             "draft",
		TargetVerification: "pending; target metadata is assumed for preview only",
		TenantID:           *tenantID,
		LegacyGroupID:      *legacyGroupID,
		Plan:               plan,
		ApplyRequirement:   "create and publish this disabled group, then apply with its exact ID and revision; apply rechecks every binding and target",
	}
	return writeJSON(stdout, response)
}

func runEmailImportApply(configPath string, args []string, stdout, stderr io.Writer) error {
	flags := commandFlags("email-import apply", stderr)
	tenantID := flags.String("tenant-id", "", "legacy group tenant ID")
	legacyGroupID := flags.String("legacy-group-id", "", "legacy notification group ID")
	nativeGroupID := flags.String("notification-group-id", "", "published native group ID")
	revision := flags.Int64("group-revision", 0, "exact published native group revision")
	expectedVersion := flags.Int64("expected-route-version", -1, "expected current source route version")
	idempotencyKey := flags.String("idempotency-key", "", "stable key reused for a retry")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || *tenantID == "" || *legacyGroupID == "" || *nativeGroupID == "" || *revision < 1 || *expectedVersion < 0 || !validOperatorIdempotencyKey(*idempotencyKey) {
		return errors.New("email import apply arguments incomplete")
	}
	bridge, err := app.NewNotificationSourceBridgeFromEnv()
	if err != nil || bridge == nil || !bridge.Enabled() {
		return errors.New("enabled notification source bridge configuration required")
	}
	defer bridge.Close()
	_, closeDB, err := openOperatorDB(configPath)
	if err != nil {
		return err
	}
	defer closeDB()
	request := service.SourceGroupProjectionRequest{
		SourceDeploymentID:  bridge.DeploymentID(),
		TenantID:            *tenantID,
		LegacyGroupID:       *legacyGroupID,
		NotificationGroupID: *nativeGroupID,
		GroupRevision:       *revision,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := bridge.SwitchToEncore(ctx, request, *idempotencyKey, *expectedVersion); err != nil {
		return errors.New("email import compatibility check or route CAS failed")
	}
	return writeJSON(stdout, struct {
		Status              string `json:"status"`
		Engine              string `json:"engine"`
		SourceDeploymentID  string `json:"sourceDeploymentId"`
		TenantID            string `json:"tenantId"`
		LegacyGroupID       string `json:"legacyGroupId"`
		NotificationGroupID string `json:"notificationGroupId"`
		GroupRevision       int64  `json:"groupRevision"`
		RouteVersion        int64  `json:"routeVersion"`
	}{"applied", "encore", bridge.DeploymentID(), *tenantID, *legacyGroupID, *nativeGroupID, *revision, *expectedVersion + 1})
}

func runRouteRollback(configPath string, args []string, stdout, stderr io.Writer) error {
	flags := commandFlags("route rollback", stderr)
	deploymentID := flags.String("deployment-id", "", "stable source deployment ID")
	tenantID := flags.String("tenant-id", "", "tenant ID")
	legacyGroupID := flags.String("legacy-group-id", "", "legacy notification group ID")
	expectedVersion := flags.Int64("expected-route-version", 0, "expected current source route version")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || *deploymentID == "" || *tenantID == "" || *legacyGroupID == "" || *expectedVersion < 1 {
		return errors.New("route rollback arguments incomplete")
	}
	_, closeDB, err := openOperatorDB(configPath)
	if err != nil {
		return err
	}
	defer closeDB()
	key := dal.SourceRouteKey{DeploymentID: *deploymentID, TenantID: *tenantID, LegacyGroup: *legacyGroupID}
	if err := dal.SwitchSourceRouteToLegacy(context.Background(), key, *expectedVersion); err != nil {
		return errors.New("source route rollback CAS failed")
	}
	return writeJSON(stdout, struct {
		Status             string `json:"status"`
		Engine             string `json:"engine"`
		SourceDeploymentID string `json:"sourceDeploymentId"`
		TenantID           string `json:"tenantId"`
		LegacyGroupID      string `json:"legacyGroupId"`
		RouteVersion       int64  `json:"routeVersion"`
	}{"rolled_back_for_future_events", "legacy", *deploymentID, *tenantID, *legacyGroupID, *expectedVersion + 1})
}

func commandFlags(name string, stderr io.Writer) *flag.FlagSet {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(stderr)
	return flags
}

func validOperatorIdempotencyKey(value string) bool {
	if len(value) < 8 || len(value) > 128 || strings.TrimSpace(value) != value {
		return false
	}
	for _, r := range value {
		if r <= 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func openOperatorDB(configPath string) (*gorm.DB, func(), error) {
	var configOption app.Option
	if configPath == "" {
		configOption = app.WithProductionConfig()
	} else {
		configOption = app.WithConfigFile(configPath)
	}
	if _, err := app.NewApplication(configOption); err != nil {
		return nil, nil, errors.New("operator configuration unavailable")
	}
	dbConfig, err := initialize.LoadDbConfig()
	if err != nil {
		return nil, nil, errors.New("operator database configuration unavailable")
	}
	dsn := fmt.Sprintf("host=%s port=%d dbname=%s user=%s password=%s sslmode=disable TimeZone=%s", dbConfig.Host, dbConfig.Port, dbConfig.DbName, dbConfig.Username, dbConfig.Password, dbConfig.TimeZone)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		return nil, nil, errors.New("operator database unavailable")
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, nil, errors.New("operator database unavailable")
	}
	sqlDB.SetMaxIdleConns(1)
	sqlDB.SetMaxOpenConns(2)
	global.DB = db
	query.SetDefault(db)
	return db, func() {
		_ = sqlDB.Close()
		global.DB = nil
		query.SetDefault(nil)
	}, nil
}

func writeJSON(dst io.Writer, value any) error {
	encoder := json.NewEncoder(dst)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return errors.New("could not write operator result")
	}
	return nil
}

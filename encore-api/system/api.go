package system

// System management API for the platform UI (R6). Super administrators only. Every action is a fixed request
// to the host-side manager (tp-agent, system role): this service never restarts, backs up, restores or upgrades
// anything itself, and no request field selects a command, image or URL. The Agent re-validates everything and
// reports progress in its own operation status, which these endpoints only relay.

import (
	"context"
	"errors"
	"regexp"
	"time"

	"encore.app/internal/systemagent"
	"encore.app/l1-access/authn"

	"encore.dev/beta/auth"
	"encore.dev/beta/errs"
	"encore.dev/rlog"
)

// agent is the client of the Agent's system socket (configured by TP_SYSTEM_SOCKET / TP_SYSTEM_TOKEN_FILE).
var agent = systemagent.FromEnv()

var (
	versionPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(\.[0-9]+)?([-.][0-9A-Za-z]+)*$`)
	backupPattern  = regexp.MustCompile(`^tp-backup-[0-9]{8}T[0-9]{6}Z-[0-9A-Za-z._-]{1,80}-full\.tar$`)
)

// currentAuth returns the authenticated caller; tests replace it because auth.Data needs the Encore runtime.
var currentAuth = func() *authn.Data {
	d, _ := auth.Data().(*authn.Data)
	return d
}

// logAction records which system action was requested (no user content, no secrets). Replaced in tests: rlog
// needs the Encore runtime.
var logAction = func(action string) { rlog.Info("system action", "action", action) }

func requireAdmin(action string) error {
	if !isSystemAdmin(currentAuth()) {
		return &errs.Error{Code: errs.PermissionDenied, Message: "super administrator required"}
	}
	logAction(action)
	return nil
}

// mapErr turns a client error into a stable API error. Agent codes are fixed identifiers and safe to show.
func mapErr(err error) error {
	var ae *systemagent.Error
	switch {
	case err == nil:
		return nil
	case errors.Is(err, systemagent.ErrUnavailable):
		return &errs.Error{Code: errs.Unavailable, Message: "system management is not available on this installation"}
	case errors.Is(err, systemagent.ErrBusy):
		return &errs.Error{Code: errs.FailedPrecondition, Message: "another system operation is running", Details: errDetails{Code: "system_busy"}}
	case errors.As(err, &ae):
		return &errs.Error{Code: errs.FailedPrecondition, Message: ae.Message, Details: errDetails{Code: ae.Code}}
	default:
		return &errs.Error{Code: errs.Internal, Message: "system manager error"}
	}
}

type errDetails struct {
	Code string `json:"code,omitempty"`
}

func (errDetails) ErrDetails() {}

// ---- status -------------------------------------------------------------------------------------------------

// StatusResponse is the health of the stack plus the current or last system operation.
type StatusResponse struct {
	AgentVersion string                  `json:"agent_version"`
	OK           bool                    `json:"ok"`
	Components   []systemagent.Component `json:"components"`
	CheckedAt    time.Time               `json:"checked_at"`
	Operation    *systemagent.Operation  `json:"operation,omitempty"`
	Upgrade      *systemagent.Upgrade    `json:"upgrade,omitempty"`
	ScopeSync    *systemagent.ScopeSync  `json:"scope_sync,omitempty"`
}

func status(ctx context.Context, c *systemagent.Client) (*StatusResponse, error) {
	r, err := c.Call(ctx, "status", nil)
	if err != nil {
		return nil, mapErr(err)
	}
	out := &StatusResponse{AgentVersion: r.AgentVersion, Operation: r.Operation, Upgrade: r.Upgrade, ScopeSync: r.ScopeSync, Components: []systemagent.Component{}}
	if r.Result != nil {
		out.OK, out.Components, out.CheckedAt = r.Result.OK, r.Result.Components, r.Result.CheckedAt
	}
	return out, nil
}

// Status reports stack health and the running operation. Super administrators only.
//
//encore:api auth method=GET path=/api/system/status
func Status(ctx context.Context) (*StatusResponse, error) {
	if err := requireAdmin("status"); err != nil {
		return nil, err
	}
	return status(ctx, agent)
}

// ---- configuration check and restart -----------------------------------------------------------------------

// CheckResponse is the result of validating the platform configuration without changing anything.
type CheckResponse struct {
	OK      bool   `json:"ok"`
	Skipped bool   `json:"skipped,omitempty"` // the backend is stopped, so nothing could be checked
	Service string `json:"service,omitempty"` // the component whose check failed
	Code    string `json:"code,omitempty"`
}

func check(ctx context.Context, c *systemagent.Client) (*CheckResponse, error) {
	r, err := c.Call(ctx, "check", nil)
	if err != nil {
		return nil, mapErr(err)
	}
	if r.Check == nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "system manager error"}
	}
	return &CheckResponse{OK: r.Check.OK, Skipped: r.Check.Skipped, Service: r.Check.Service, Code: r.Check.Code}, nil
}

// CheckConfig validates the configuration (the restart dialog runs it first). Super administrators only.
//
//encore:api auth method=POST path=/api/system/check-config
func CheckConfig(ctx context.Context) (*CheckResponse, error) {
	if err := requireAdmin("check-config"); err != nil {
		return nil, err
	}
	return check(ctx, agent)
}

// OperationResponse is returned by every action that starts an asynchronous operation.
type OperationResponse struct {
	Operation *systemagent.Operation `json:"operation"`
}

// RestartRequest selects the restart scope.
type RestartRequest struct {
	Scope string `json:"scope"` // "control" (platform services only; devices stay connected) or "full" (also the broker)
}

func startOperation(ctx context.Context, c *systemagent.Client, action string, body map[string]any) (*OperationResponse, error) {
	r, err := c.Call(ctx, action, body)
	if err != nil {
		return nil, mapErr(err)
	}
	if r.Operation == nil {
		return nil, &errs.Error{Code: errs.Internal, Message: "system manager error"}
	}
	return &OperationResponse{Operation: r.Operation}, nil
}

func restart(ctx context.Context, c *systemagent.Client, scope string) (*OperationResponse, error) {
	if scope != "control" && scope != "full" {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "scope must be control or full"}
	}
	return startOperation(ctx, c, "restart", map[string]any{"scope": scope})
}

// Restart restarts the platform in the background; progress is in Status.operation and on the maintenance page.
// Super administrators only.
//
//encore:api auth method=POST path=/api/system/restart
func Restart(ctx context.Context, req *RestartRequest) (*OperationResponse, error) {
	if err := requireAdmin("restart"); err != nil {
		return nil, err
	}
	return restart(ctx, agent, req.Scope)
}

// ---- updates and upgrade ----------------------------------------------------------------------------------

// UpdateItem is one entry of the update list.
type UpdateItem struct {
	Kind         string    `json:"kind"` // "platform" | "agent"
	Name         string    `json:"name"`
	Current      string    `json:"current"`
	Latest       string    `json:"latest,omitempty"`
	UpdateAvail  bool      `json:"update_available"`
	CanUpgrade   bool      `json:"can_upgrade"`            // the UI offers [Upgrade] only for the platform
	Instructions string    `json:"instructions,omitempty"` // for items the UI cannot upgrade itself
	ValidUntil   time.Time `json:"valid_until,omitempty"`
}

// UpdatesResponse is the result of "check for updates". Plugins join this list in R7.
type UpdatesResponse struct {
	Items []UpdateItem `json:"items"`
	Error string       `json:"error,omitempty"` // fixed code when the platform release could not be checked
}

func updates(ctx context.Context, c *systemagent.Client) (*UpdatesResponse, error) {
	st, err := c.Call(ctx, "status", nil)
	if err != nil {
		return nil, mapErr(err)
	}
	out := &UpdatesResponse{Items: []UpdateItem{}}
	r, err := c.Call(ctx, "updates", nil)
	switch {
	case err == nil && r.Updates != nil:
		out.Items = append(out.Items, UpdateItem{Kind: "platform", Name: "ThingsPanel", Current: r.Updates.Current, Latest: r.Updates.Latest,
			UpdateAvail: r.Updates.Newer, CanUpgrade: r.Updates.Newer, ValidUntil: r.Updates.ValidTo})
	case err != nil:
		var ae *systemagent.Error
		if errors.As(err, &ae) {
			out.Error = ae.Code
		} else {
			return nil, mapErr(err)
		}
	}
	// The Agent is upgraded with the installer in this phase; the UI only says how.
	out.Items = append(out.Items, UpdateItem{Kind: "agent", Name: "tp-agent", Current: st.AgentVersion, Instructions: "upgrade_with_installer"})
	return out, nil
}

// Updates reads the signed release manifest and lists what can be upgraded. Super administrators only.
//
//encore:api auth method=GET path=/api/system/updates
func Updates(ctx context.Context) (*UpdatesResponse, error) {
	if err := requireAdmin("updates"); err != nil {
		return nil, err
	}
	return updates(ctx, agent)
}

// UpgradeRequest names the platform version to upgrade to; it must match the signed release.
type UpgradeRequest struct {
	To string `json:"to"`
}

func upgrade(ctx context.Context, c *systemagent.Client, to string) (*OperationResponse, error) {
	if !versionPattern.MatchString(to) {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "invalid version"}
	}
	return startOperation(ctx, c, "upgrade", map[string]any{"to": to})
}

// Upgrade starts a platform upgrade (a backup is always taken first; a failed upgrade is rolled back
// automatically). Super administrators only.
//
//encore:api auth method=POST path=/api/system/upgrade
func Upgrade(ctx context.Context, req *UpgradeRequest) (*OperationResponse, error) {
	if err := requireAdmin("upgrade"); err != nil {
		return nil, err
	}
	return upgrade(ctx, agent, req.To)
}

// ---- backups -----------------------------------------------------------------------------------------------

// BackupsResponse lists backup packages, newest first as the Agent returns them.
type BackupsResponse struct {
	Backups []systemagent.Backup `json:"backups"`
}

func backups(ctx context.Context, c *systemagent.Client) (*BackupsResponse, error) {
	r, err := c.Call(ctx, "backup/list", nil)
	if err != nil {
		return nil, mapErr(err)
	}
	if r.Backups == nil {
		r.Backups = []systemagent.Backup{}
	}
	return &BackupsResponse{Backups: r.Backups}, nil
}

// ListBackups lists backup packages. Super administrators only.
//
//encore:api auth method=GET path=/api/system/backups
func ListBackups(ctx context.Context) (*BackupsResponse, error) {
	if err := requireAdmin("backups"); err != nil {
		return nil, err
	}
	return backups(ctx, agent)
}

// CreateBackup starts a manual backup in the background. Super administrators only.
//
//encore:api auth method=POST path=/api/system/backups
func CreateBackup(ctx context.Context) (*OperationResponse, error) {
	if err := requireAdmin("backup-create"); err != nil {
		return nil, err
	}
	return startOperation(ctx, agent, "backup/create", nil)
}

func backupAction(ctx context.Context, c *systemagent.Client, action, name string) (*OperationResponse, error) {
	if !backupPattern.MatchString(name) {
		return nil, &errs.Error{Code: errs.InvalidArgument, Message: "invalid backup name"}
	}
	if action == "backup/delete" {
		if _, err := c.Call(ctx, action, map[string]any{"name": name}); err != nil {
			return nil, mapErr(err)
		}
		return &OperationResponse{}, nil
	}
	return startOperation(ctx, c, action, map[string]any{"name": name})
}

// RestoreBackup restores a backup package (the data goes back to the moment of the backup; a safety snapshot
// is taken first and a failed restore is rolled back). Super administrators only.
//
//encore:api auth method=POST path=/api/system/backups/:name/restore
func RestoreBackup(ctx context.Context, name string) (*OperationResponse, error) {
	if err := requireAdmin("backup-restore"); err != nil {
		return nil, err
	}
	return backupAction(ctx, agent, "backup/restore", name)
}

// DeleteBackup deletes a backup package. Super administrators only.
//
//encore:api auth method=DELETE path=/api/system/backups/:name
func DeleteBackup(ctx context.Context, name string) (*OperationResponse, error) {
	if err := requireAdmin("backup-delete"); err != nil {
		return nil, err
	}
	return backupAction(ctx, agent, "backup/delete", name)
}

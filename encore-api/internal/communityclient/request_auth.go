package communityclient

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

type authenticatedReadKey struct{}

type authenticatedRead struct {
	token        string
	command      bool
	diagnostic   bool
	cancelDevice string
	cancelID     string
}

// ErrRequestAuthentication is intentionally independent of service credentials:
// a missing caller credential must never fall back to a more privileged account.
var ErrRequestAuthentication = errors.New("authenticated runtime read requires one unambiguous caller token")

// WithAuthenticatedReadRequest carries an already authenticated incoming
// request's credential only through its synchronous runtime reads. The caller
// must still perform native tenant, resource and capability checks before using
// the client. This function does not authenticate the token itself; Community
// independently authenticates and authorizes the forwarded request.
//
// Even invalid/missing credentials set a marker, preventing service-account
// fallback. Neither headers nor credentials are retained on the shared Client.
// Do not attach this context to background work or command submissions.
func WithAuthenticatedReadRequest(ctx context.Context, r *http.Request) context.Context {
	credential := authenticatedRead{}
	if r != nil {
		credential.token = readRequestToken(r.Header)
	}
	return context.WithValue(ctx, authenticatedReadKey{}, credential)
}

func readRequestToken(h http.Header) string {
	authorization, legacy := h.Values("Authorization"), h.Values("x-token")
	if len(authorization) > 1 || len(legacy) > 1 {
		return ""
	}
	var bearer, token string
	if len(authorization) == 1 {
		parts := strings.Fields(authorization[0])
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			return ""
		}
		bearer = parts[1]
	}
	if len(legacy) == 1 {
		token = legacy[0]
		if !validRequestToken(token) {
			return ""
		}
	}
	if bearer != "" {
		if token != "" && token != bearer {
			return ""
		}
		token = bearer
	}
	if !validRequestToken(token) {
		return ""
	}
	return token
}

func validRequestToken(token string) bool {
	if len(token) == 0 || len(token) > 16384 {
		return false
	}
	for _, b := range []byte(token) {
		if b <= 32 || b >= 127 || b == ',' {
			return false
		}
	}
	return true
}

// WithAuthenticatedCommandRequest is restricted to synchronous native command
// submission after native tenant, device and command-capability validation.
// The credential lives only in the request context and is never persisted for workers.
func WithAuthenticatedCommandRequest(ctx context.Context, r *http.Request) context.Context {
	credential := authenticatedRead{command: true}
	if r != nil {
		credential.token = readRequestToken(r.Header)
	}
	return context.WithValue(ctx, authenticatedReadKey{}, credential)
}

// WithAuthenticatedDiagnosticRequest forwards only the synchronous caller token
// after native tenant/resource/diagnose checks. No worker credential is stored.
func WithAuthenticatedDiagnosticRequest(ctx context.Context, r *http.Request) context.Context {
	credential := authenticatedRead{diagnostic: true}
	if r != nil {
		credential.token = readRequestToken(r.Header)
	}
	return context.WithValue(ctx, authenticatedReadKey{}, credential)
}

// WithAuthenticatedCommandCancellationRequest scopes synchronous cancellation to
// an already tenant-validated device and runtime expected-message identifier.
func WithAuthenticatedCommandCancellationRequest(ctx context.Context, r *http.Request, deviceID, expectedID string) context.Context {
	a := authenticatedRead{cancelDevice: deviceID, cancelID: expectedID}
	if r != nil && deviceID != "" && expectedID != "" {
		a.token = readRequestToken(r.Header)
	}
	return context.WithValue(ctx, authenticatedReadKey{}, a)
}

func (a authenticatedRead) permits(method, path string) bool {
	if a.token == "" {
		return false
	}
	if a.cancelDevice != "" || a.cancelID != "" {
		if method == http.MethodDelete {
			return path == "/api/v1/expected/data/"+url.PathEscape(a.cancelID)
		}
		u, err := url.Parse(path)
		if err != nil || method != http.MethodGet || u.Path != "/api/v1/expected/data/list" {
			return false
		}
		q := u.Query()
		return len(q["device_id"]) == 1 && q.Get("device_id") == a.cancelDevice && len(q["send_type"]) == 1 && q.Get("send_type") == "command"
	}
	if a.diagnostic {
		if method == http.MethodPost && path == "/api/v1/telemetry/datas/simulation/send" {
			return true
		}
		parts := strings.Split(path, "/")
		return len(parts) >= 6 && parts[1] == "api" && parts[2] == "v1" && parts[3] == "device" && parts[4] != "" && !strings.Contains(parts[4], "%") && parts[5] == "debug" && ((len(parts) == 6 && method == http.MethodPost) || (len(parts) == 7 && parts[6] == "status" && method == http.MethodGet))
	}
	if !a.command {
		return method == http.MethodGet || (method == http.MethodPost && (path == "/api/v1/device/status/evidence/batch" || path == "/v1/device-alarm-summaries"))
	}
	return method == http.MethodPost && (path == "/api/v1/command/datas/pub" || path == "/api/v1/expected/data")
}

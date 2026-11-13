package communityclient

import (
	"context"
	"errors"
	"net/http"
	"strings"
)

type authenticatedReadKey struct{}

type authenticatedRead struct {
	token string
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

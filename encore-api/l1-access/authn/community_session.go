package authn

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"encore.dev/beta/errs"
)

const (
	communitySessionPath       = "/api/v1/user/detail"
	communitySessionTimeout    = 2 * time.Second
	communitySessionMaxBody    = 256 << 10
	communitySessionRejectText = "invalid or expired token"
)

type communitySessionEnvelope struct {
	Code *int            `json:"code"`
	Data json.RawMessage `json:"data"`
}

type communitySessionUser struct {
	ID        string `json:"id"`
	TenantID  string `json:"tenant_id"`
	Authority string `json:"authority"`
	Status    string `json:"status"`
}

// checkCommunitySession verifies the signed token against the configured
// Community endpoint on every request. It deliberately does not cache success.
func checkCommunitySession(ctx context.Context, baseURL, token string, expected *Data) error {
	endpoint, err := communitySessionEndpoint(baseURL)
	if err != nil {
		return rejectCommunitySession("configuration")
	}
	if ctx == nil || strings.TrimSpace(token) == "" || expected == nil ||
		strings.TrimSpace(expected.UserID) == "" || strings.TrimSpace(expected.TenantID) == "" || len(expected.Roles) != 1 || strings.TrimSpace(expected.Roles[0]) == "" {
		return rejectCommunitySession("identity")
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return rejectCommunitySession("request")
	}
	request.Header.Set("x-token", token)

	client := &http.Client{
		Timeout: communitySessionTimeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	response, err := client.Do(request)
	if err != nil {
		return rejectCommunitySession("request")
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return rejectCommunitySession("http_status")
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, communitySessionMaxBody+1))
	if err != nil || len(body) > communitySessionMaxBody {
		return rejectCommunitySession("response_body")
	}
	var envelope communitySessionEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.Code == nil || (*envelope.Code != 200 && *envelope.Code != 0) || len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return rejectCommunitySession("response_envelope")
	}
	var user communitySessionUser
	if err := json.Unmarshal(envelope.Data, &user); err != nil {
		return rejectCommunitySession("response_data")
	}
	if strings.TrimSpace(user.ID) != strings.TrimSpace(expected.UserID) ||
		strings.TrimSpace(user.TenantID) != strings.TrimSpace(expected.TenantID) ||
		strings.TrimSpace(user.Authority) != strings.TrimSpace(expected.Roles[0]) ||
		strings.TrimSpace(user.Status) != "N" {
		return rejectCommunitySession("identity_or_status")
	}
	return nil
}

func communitySessionEndpoint(baseURL string) (string, error) {
	base, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || base == nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" ||
		base.User != nil || base.RawQuery != "" || base.ForceQuery || base.Fragment != "" || base.RawFragment != "" ||
		(base.Path != "" && base.Path != "/") || base.Opaque != "" {
		return "", errors.New("invalid Community session base URL")
	}
	base.Path = communitySessionPath
	base.RawPath = ""
	return base.String(), nil
}

func rejectCommunitySession(reason string) error {
	// Keep logs useful without exposing the bearer token or any response data.
	log.Printf("[ERROR] legacy JWT Community session check failed: %s", reason)
	return &errs.Error{Code: errs.Unauthenticated, Message: communitySessionRejectText}
}

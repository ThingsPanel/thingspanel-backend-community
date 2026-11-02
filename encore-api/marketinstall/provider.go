package marketinstall

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"encore.app/marketinstall/app"
)

type httpProvider struct {
	base, token string
	client      *http.Client
}

type providerStatusError struct {
	Action string
	Status int
}

func (e *providerStatusError) Error() string {
	return fmt.Sprintf("marketinstall provider %s returned status %d", e.Action, e.Status)
}

func newHTTPProvider(base, token string) (*httpProvider, error) {
	parsed, err := url.Parse(base)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("marketinstall provider configuration is incomplete")
	}
	return &httpProvider{base: strings.TrimRight(base, "/"), token: token, client: &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func (p *httpProvider) call(ctx context.Context, action string, input, output interface{}) error {
	raw, err := json.Marshal(input)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.base+"/internal/market-install/"+action, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-TP-Service-Token", p.token)
	response, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	// Never log authorization references, tokens, or raw provider error bodies.
	if response.StatusCode != http.StatusOK {
		return &providerStatusError{Action: action, Status: response.StatusCode}
	}
	limited := io.LimitReader(response.Body, 8*1024*1024+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return err
	}
	if len(body) > 8*1024*1024 {
		return fmt.Errorf("marketinstall provider response exceeds limit")
	}
	if err := json.Unmarshal(body, output); err != nil {
		return fmt.Errorf("marketinstall provider response invalid")
	}
	return nil
}
func (p *httpProvider) Prepare(ctx context.Context, req app.PrepareRequest) (app.Plan, error) {
	var plan app.Plan
	err := p.call(ctx, "prepare", req, &plan)
	return plan, err
}
func (p *httpProvider) Query(ctx context.Context, req app.StepRequest) (app.Observation, error) {
	var observation app.Observation
	err := p.call(ctx, "query", req, &observation)
	return observation, err
}
func (p *httpProvider) Execute(ctx context.Context, req app.StepRequest) (app.Observation, error) {
	var observation app.Observation
	err := p.call(ctx, "execute", req, &observation)
	return observation, err
}

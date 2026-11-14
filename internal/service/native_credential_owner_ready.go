package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Reuse the deployed broker's Owner identity; never accept the token from a
// browser or expose it in diagnostic errors. No readiness is cached across boots.
func RequireNativeCredentialOwnerReady(ctx context.Context) error {
	unavailable := errors.New("broker credential lease protocol unavailable")
	endpoint := strings.TrimRight(os.Getenv("TP_BROKER_OWNER_URL"), "/")
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return unavailable
	}
	token, err := os.ReadFile(os.Getenv("TP_BROKER_TOKEN_FILE"))
	if err != nil {
		return unavailable
	}
	secret := strings.TrimSpace(string(token))
	cell, instance := os.Getenv("TP_BROKER_CELL"), os.Getenv("TP_BROKER_INSTANCE")
	if len(secret) < 32 || len(secret) > 4096 || cell == "" || instance == "" || strings.ContainsAny(secret, " \t\r\n") {
		return unavailable
	}
	raw, _ := json.Marshal(map[string]string{"broker_cell": cell, "broker_instance": instance})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/v1/credential-leases/ready", bytes.NewReader(raw))
	if err != nil {
		return unavailable
	}
	req.Header.Set("Authorization", "Bearer "+secret)
	req.Header.Set("Content-Type", "application/json")
	client := http.Client{Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return unavailable
	}
	defer resp.Body.Close()
	var result struct {
		Protocol int  `json:"protocol"`
		Ready    bool `json:"ready"`
	}
	dec := json.NewDecoder(io.LimitReader(resp.Body, 2048))
	dec.DisallowUnknownFields()
	if resp.StatusCode != 200 || dec.Decode(&result) != nil || dec.Decode(new(any)) != io.EOF || result.Protocol != 1 || !result.Ready {
		return unavailable
	}
	return nil
}

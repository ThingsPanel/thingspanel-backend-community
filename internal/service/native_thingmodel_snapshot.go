package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const maxNativeSnapshotResponseBytes = 1 << 20

type nativeSnapshotDetail struct {
	ModelID           string `json:"modelID"`
	PublishedRevision int    `json:"publishedRevision"`
	SnapshotID        string `json:"snapshotID"`
}

// validateNativeSnapshot forwards the authenticated ThingsPanel token to the
// native service. The native service resolves the caller's tenant and only
// returns snapshots visible to that tenant.
func validateNativeSnapshot(ctx context.Context, token, modelID string, revision int, snapshotID string) error {
	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("TP_THINGMODEL_NATIVE_URL")), "/")
	if baseURL == "" {
		return fmt.Errorf("native thing-model service URL is not configured")
	}
	parsedBase, err := url.Parse(baseURL)
	if err != nil || (parsedBase.Scheme != "http" && parsedBase.Scheme != "https") || parsedBase.Host == "" || parsedBase.User != nil || parsedBase.RawQuery != "" || parsedBase.Fragment != "" {
		return fmt.Errorf("native thing-model service URL is invalid")
	}
	if strings.TrimSpace(token) == "" {
		return fmt.Errorf("native thing-model binding requires an authenticated user token")
	}
	path := "/api/v2/thing-models/" + url.PathEscape(modelID) + "/versions/" + strconv.Itoa(revision)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("build native snapshot request: %w", err)
	}
	token = strings.TrimSpace(token)
	if strings.HasPrefix(strings.ToLower(token), "bearer ") {
		token = strings.TrimSpace(token[7:])
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("native thing-model service request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("native thing-model snapshot is unavailable (status %d)", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxNativeSnapshotResponseBytes+1))
	if err != nil {
		return fmt.Errorf("read native thing-model snapshot: %w", err)
	}
	if len(body) > maxNativeSnapshotResponseBytes {
		return fmt.Errorf("native thing-model snapshot response is too large")
	}
	var detail nativeSnapshotDetail
	if err := json.Unmarshal(body, &detail); err != nil {
		return fmt.Errorf("native thing-model snapshot response is invalid")
	}
	if detail.ModelID != modelID || detail.PublishedRevision != revision || detail.SnapshotID != snapshotID {
		return fmt.Errorf("native thing-model snapshot does not match the requested binding")
	}
	return nil
}

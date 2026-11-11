package automationbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

type TickReceipt struct {
	Replayed int
	Counters map[string]int
}

// Tick is deployment-scoped: a browser tenant identity is never forwarded to
// the scheduler. Recover and Tick run in the native owner's existing endpoint.
func (c *Client) Tick(ctx context.Context) (*TickReceipt, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url+"/internal/automation/tick", bytes.NewBufferString("{}"))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set(DeploymentHeader, c.deployment)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("native scheduler HTTP %d", resp.StatusCode)
	}
	var receipt struct {
		Replayed *int           `json:"replayed"`
		Tick     map[string]int `json:"tick"`
	}
	decoder := json.NewDecoder(io.LimitReader(resp.Body, 1<<20))
	if decoder.Decode(&receipt) != nil || receipt.Replayed == nil || *receipt.Replayed < 0 || receipt.Tick == nil {
		return nil, errors.New("invalid native scheduler receipt")
	}
	for _, key := range []string{"schedule_materialized", "schedule_executed", "outbox_drained"} {
		if _, exists := receipt.Tick[key]; !exists {
			return nil, errors.New("incomplete native scheduler receipt")
		}
	}
	for _, count := range receipt.Tick {
		if count < 0 {
			return nil, errors.New("invalid native scheduler counter")
		}
	}
	var extra interface{}
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, errors.New("invalid trailing native scheduler response")
	}
	return &TickReceipt{Replayed: *receipt.Replayed, Counters: receipt.Tick}, nil
}

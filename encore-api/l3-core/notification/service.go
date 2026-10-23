package notification

import (
	"context"
	"net/http"
)

//encore:service
type Service struct {
	api    http.Handler
	store  *Store
	worker *Worker
}

// Shutdown drains the bounded worker before Encore closes its database pool.
func (s *Service) Shutdown(ctx context.Context) {
	if s.worker != nil {
		_ = s.worker.Stop(ctx)
	}
}

type HistoryRequest struct {
	Page     int `query:"page"`
	PageSize int `query:"page_size"`
}

type HistoryItem struct {
	ID        string `json:"id"`
	RuleID    string `json:"rule_id"`
	DeviceID  string `json:"device_id"`
	Message   string `json:"message"`
	CreatedAt string `json:"created_at"`
}

type HistoryData struct {
	Items    []HistoryItem `json:"items"`
	Page     int           `json:"page"`
	PageSize int           `json:"page_size"`
	Total    int           `json:"total"`
}

type HistoryEnvelope struct {
	Code      int         `json:"code"`
	Message   string      `json:"message"`
	RequestID string      `json:"requestId"`
	Data      HistoryData `json:"data"`
}

//encore:api public raw method=GET path=/v1/notifications/history
func (s *Service) History(w http.ResponseWriter, r *http.Request) { s.api.ServeHTTP(w, r) }

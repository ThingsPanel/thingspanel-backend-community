package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseExistsFromBody(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		want      bool
		wantError bool
	}{
		{
			name: "flat exists",
			body: `{"exists":true,"email":"demo@example.com"}`,
			want: true,
		},
		{
			name: "nested exists in data",
			body: `{"code":200,"data":{"exists":false}}`,
			want: false,
		},
		{
			name: "boolean data",
			body: `{"code":200,"data":true}`,
			want: true,
		},
		{
			name:      "missing exists",
			body:      `{"code":200,"data":{"email":"demo@example.com"}}`,
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseExistsFromBody([]byte(tt.body))
			if tt.wantError {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("expected %v, got %v", tt.want, got)
			}
		})
	}
}

func TestCheckUserExists_ResponseClassify(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		body      string
		want      bool
		wantError error
	}{
		{
			name:   "not found",
			status: http.StatusNotFound,
			body:   `{"message":"not found"}`,
			want:   false,
		},
		{
			name:   "bad request but user not exists",
			status: http.StatusBadRequest,
			body:   `{"message":"email not found"}`,
			want:   false,
		},
		{
			name:      "non-200 rejected",
			status:    http.StatusInternalServerError,
			body:      `{"message":"internal error"}`,
			wantError: ErrMarketRequestRejected,
		},
		{
			name:      "invalid body",
			status:    http.StatusOK,
			body:      `not-json`,
			wantError: ErrMarketInvalidResponse,
		},
		{
			name:   "ok exists",
			status: http.StatusOK,
			body:   `{"exists":true}`,
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			client := &MarketClient{
				baseURL:    server.URL,
				httpClient: server.Client(),
			}

			got, err := client.CheckUserExists(context.Background(), "demo@example.com")
			if tt.wantError != nil {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if !errors.Is(err, tt.wantError) {
					t.Fatalf("expected error %v, got %v", tt.wantError, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("expected %v, got %v", tt.want, got)
			}
		})
	}
}

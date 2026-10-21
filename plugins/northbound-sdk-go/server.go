package northbound

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"time"
)

// Sink is what a connector author implements.
type Sink interface {
	// Describe declares identity and honest capabilities.
	Describe() Descriptor
	// Validate checks configuration and credentials without sending data.
	Validate(ctx context.Context, config json.RawMessage, secrets map[string]string) ValidationResult
	// Open prepares a connection and returns a handle used by Write.
	Open(ctx context.Context, req OpenRequest) (OpenResponse, error)
	// Write sends one batch and returns one result per item.
	Write(ctx context.Context, req WriteRequest) ([]WriteResult, error)
	// Close releases a connection handle.
	Close(ctx context.Context, instanceID string) error
	// Health reports whether the process is able to serve.
	Health(ctx context.Context) error
}

// ServerOptions configures the HTTP surface.
type ServerOptions struct {
	// Token is the plugin-internal bearer credential the platform must present.
	// An empty token closes the server: an unauthenticated connector port would
	// be an open relay into the customer's target system.
	Token  string
	Logger *log.Logger
}

// NewServer returns the northbound-connector/v1 HTTP handler.
func NewServer(sink Sink, opts ServerOptions) http.Handler {
	if opts.Logger == nil {
		opts.Logger = log.Default()
	}
	mux := http.NewServeMux()
	auth := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if opts.Token == "" {
				writeError(w, http.StatusServiceUnavailable, "connector token is not configured")
				return
			}
			got := r.Header.Get("Authorization")
			if len(got) > 7 && got[:7] == "Bearer " {
				got = got[7:]
			}
			if subtle.ConstantTimeCompare([]byte(got), []byte(opts.Token)) != 1 {
				writeError(w, http.StatusUnauthorized, "invalid platform credential")
				return
			}
			next(w, r)
		}
	}
	mux.HandleFunc("/v1/describe", auth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "GET only")
			return
		}
		writeJSON(w, http.StatusOK, sink.Describe())
	}))
	mux.HandleFunc("/v1/health", auth(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		if err := sink.Health(ctx); err != nil {
			writeError(w, http.StatusServiceUnavailable, "unhealthy")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}))
	mux.HandleFunc("/v1/validate", auth(func(w http.ResponseWriter, r *http.Request) {
		var req ValidateRequest
		if !decode(w, r, &req) {
			return
		}
		writeJSON(w, http.StatusOK, sink.Validate(r.Context(), req.Config, req.Secrets))
	}))
	mux.HandleFunc("/v1/open", auth(func(w http.ResponseWriter, r *http.Request) {
		var req OpenRequest
		if !decode(w, r, &req) {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		resp, err := sink.Open(ctx, req)
		if err != nil {
			// A failed open is a configuration problem, not a transient one.
			writeJSON(w, http.StatusOK, OpenResponse{OK: false, Message: err.Error()})
			return
		}
		if resp.InstanceID == "" {
			resp.InstanceID = req.ConnectionID
		}
		resp.OK = true
		writeJSON(w, http.StatusOK, resp)
	}))
	mux.HandleFunc("/v1/write", auth(func(w http.ResponseWriter, r *http.Request) {
		var req WriteRequest
		if !decode(w, r, &req) {
			return
		}
		if len(req.Items) == 0 {
			writeJSON(w, http.StatusOK, WriteResponse{ContractVersion: ContractVersion, Results: []WriteResult{}})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
		defer cancel()
		results, err := sink.Write(ctx, req)
		if err != nil {
			// The whole batch failed before any item was accepted. Every item
			// gets an explicit result so the platform never has to guess.
			out := make([]WriteResult, 0, len(req.Items))
			for _, it := range req.Items {
				out = append(out, WriteResult{
					DeliveryID: it.DeliveryID, Status: StatusRetryable,
					ErrorCode: "write_failed", ErrorDetail: err.Error(),
				})
			}
			writeJSON(w, http.StatusOK, WriteResponse{ContractVersion: ContractVersion, Results: out})
			return
		}
		if len(results) != len(req.Items) {
			opts.Logger.Printf("northbound connector: write returned %d results for %d items", len(results), len(req.Items))
		}
		writeJSON(w, http.StatusOK, WriteResponse{ContractVersion: ContractVersion, Results: results})
	}))
	mux.HandleFunc("/v1/close", auth(func(w http.ResponseWriter, r *http.Request) {
		var req CloseRequest
		if !decode(w, r, &req) {
			return
		}
		if err := sink.Close(r.Context(), req.InstanceID); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}))
	return mux
}

func decode(w http.ResponseWriter, r *http.Request, out any) bool {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "POST only")
		return false
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "unreadable body")
		return false
	}
	if len(raw) == 0 {
		writeError(w, http.StatusBadRequest, "empty body")
		return false
	}
	if err := json.Unmarshal(raw, out); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// ErrNotConfigured is a convenient sentinel for connectors that were opened
// without required configuration.
var ErrNotConfigured = errors.New("connector is not configured")

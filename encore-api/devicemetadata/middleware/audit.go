package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// AuditAction represents the action being audited
type AuditAction string

const (
	ActionCreated AuditAction = "CREATED"
	ActionUpdated AuditAction = "UPDATED"
	ActionDeleted AuditAction = "DELETED"
)

// AuditLogEntry represents a single audit log entry
type AuditLogEntry struct {
	ID         string                 `json:"id"`
	TenantID   string                 `json:"tenant_id"`
	ActorID    string                 `json:"actor_id"`
	Action     string                 `json:"action"`
	TargetType string                 `json:"target_type"`
	TargetID   string                 `json:"target_id"`
	RequestID  string                 `json:"request_id"`
	Details    map[string]interface{} `json:"details,omitempty"`
	CreatedAt  time.Time              `json:"created_at"`
}

// AuditLogger manages audit log entries
type AuditLogger struct {
	entries chan *AuditLogEntry
	wg      sync.WaitGroup
	done    chan struct{}
}

// NewAuditLogger creates a new audit logger with buffered channel
func NewAuditLogger(bufferSize int) *AuditLogger {
	logger := &AuditLogger{
		entries: make(chan *AuditLogEntry, bufferSize),
		done:    make(chan struct{}),
	}

	logger.wg.Add(1)
	go func() {
		defer logger.wg.Done()
		for {
			select {
			case <-logger.done:
				return
			case entry := <-logger.entries:
				if entry != nil {
					// In production, write to database
					// For now, this is where async writes would happen
					_ = entry
				}
			}
		}
	}()

	return logger
}

// Log asynchronously logs an audit entry
func (al *AuditLogger) Log(entry *AuditLogEntry) {
	select {
	case al.entries <- entry:
	case <-al.done:
		// Logger is shutting down, silently drop
	}
}

// Close gracefully shuts down the logger
func (al *AuditLogger) Close() {
	close(al.done)
	al.wg.Wait()
}

// DeriveAction derives the audit action from HTTP method and path
func DeriveAction(method, path string) AuditAction {
	switch method {
	case "POST":
		return ActionCreated
	case "PUT", "PATCH":
		return ActionUpdated
	case "DELETE":
		return ActionDeleted
	default:
		return ""
	}
}

// DeriveTargetType derives the target type from request path
// Examples: /api/thing-models/{id} -> THING_MODEL
//           /api/devices/{id} -> DEVICE
//           /api/thing-models -> THING_MODEL
func DeriveTargetType(path string) string {
	path = strings.Trim(path, "/")
	if path == "" {
		return ""
	}

	parts := strings.Split(path, "/")

	// Find the resource segment (usually after /api/)
	// Expected formats: /api/thing-models or /api/thing-models/123
	var resourcePart string

	if len(parts) >= 2 && parts[0] == "api" {
		// Get the next segment after "api"
		resourcePart = parts[1]
	} else if len(parts) == 1 {
		// Direct path without /api/ prefix
		resourcePart = parts[0]
	} else {
		return ""
	}

	// Convert kebab-case to UPPER_SNAKE_CASE
	resourcePart = strings.ReplaceAll(resourcePart, "-", "_")

	// Singularize common plurals
	resourcePart = singularize(resourcePart)

	return strings.ToUpper(resourcePart)
}

// singularize converts common plural forms to singular
func singularize(s string) string {
	if s == "" {
		return s
	}

	// Special cases
	if strings.HasSuffix(s, "ies") && len(s) > 3 {
		return strings.TrimSuffix(s, "ies") + "y"
	}
	if strings.HasSuffix(s, "ies") {
		return s
	}

	// For words ending in -es, only remove if it looks like a plural
	if strings.HasSuffix(s, "ches") || strings.HasSuffix(s, "shes") ||
		strings.HasSuffix(s, "xes") || strings.HasSuffix(s, "zes") {
		if len(s) > 2 {
			return strings.TrimSuffix(s, "es")
		}
	}

	// For words ending in -s, only remove if certain
	// Don't remove for short words or words that would look wrong
	if strings.HasSuffix(s, "s") && len(s) > 2 && !strings.HasSuffix(s, "ss") {
		// Check if removing 's' leaves a valid word
		singular := strings.TrimSuffix(s, "s")
		// Don't pluralize words that end in vowels followed by s
		if len(singular) > 0 {
			lastChar := rune(singular[len(singular)-1])
			if lastChar != 'u' { // Keep 'us' words as they are
				return singular
			}
		}
	}

	return s
}

// IsMutatingRequest checks if a request is mutating (POST/PUT/DELETE)
func IsMutatingRequest(method string) bool {
	return method == "POST" || method == "PUT" || method == "PATCH" || method == "DELETE"
}

// SanitizeDetails removes sensitive fields from request body for logging
func SanitizeDetails(details map[string]interface{}) map[string]interface{} {
	sensitiveFields := []string{"password", "token", "voucher", "secret", "api_key", "access_token", "refresh_token"}

	sanitized := make(map[string]interface{})
	for key, value := range details {
		keyLower := strings.ToLower(key)
		isSensitive := false
		for _, sensitive := range sensitiveFields {
			if keyLower == sensitive || strings.Contains(keyLower, sensitive) {
				isSensitive = true
				break
			}
		}

		if !isSensitive {
			sanitized[key] = value
		}
	}

	return sanitized
}

// NewAuditLogEntry creates a new audit log entry
func NewAuditLogEntry(tenantID, actorID, action, targetType, targetID, requestID string, details map[string]interface{}) *AuditLogEntry {
	return &AuditLogEntry{
		ID:        uuid.New().String(),
		TenantID:  tenantID,
		ActorID:   actorID,
		Action:    action,
		TargetType: targetType,
		TargetID:  targetID,
		RequestID: requestID,
		Details:   SanitizeDetails(details),
		CreatedAt: time.Now().UTC(),
	}
}

// ToJSON converts the audit log entry to JSON
func (ale *AuditLogEntry) ToJSON() (string, error) {
	data, err := json.Marshal(ale)
	if err != nil {
		return "", fmt.Errorf("failed to marshal audit log entry: %w", err)
	}
	return string(data), nil
}

// WithRequestID injects request_id into context
func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDKey, requestID)
}

// RequestIDFromCtx extracts request_id from context
func RequestIDFromCtx(ctx context.Context) (string, bool) {
	requestID, ok := ctx.Value(requestIDKey).(string)
	return requestID, ok
}

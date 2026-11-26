package sdk

import (
	"context"
	"errors"
	"net/http"
	"os"
)

// Unconfigured publishes the official manifest before provider credentials exist.
type Unconfigured struct{ Definition Manifest }

var errProviderNotConfigured = errors.New("provider configuration required")

func (p Unconfigured) Manifest() Manifest { return p.Definition }
func (p Unconfigured) Health(context.Context) error {
	return errProviderNotConfigured
}
func (p Unconfigured) Validate(context.Context, ValidateRequest) ValidateResult {
	return ValidateResult{Errors: []FieldError{{Field: "config", Code: "provider_not_configured", Message: "Configure the notification provider first."}}}
}
func (p Unconfigured) Send(_ context.Context, req SendRequest) (SendResult, error) {
	return SendResult{DeliveryID: req.DeliveryID, Outcome: "rejected", Error: &DeliveryError{Code: "provider_not_configured", Message: "Configure the notification provider first."}}, nil
}

func Serve(server *http.Server) error {
	cert, key := os.Getenv("NOTIFICATION_TLS_CERT"), os.Getenv("NOTIFICATION_TLS_KEY")
	if cert != "" || key != "" {
		return server.ListenAndServeTLS(cert, key)
	}
	return server.ListenAndServe()
}

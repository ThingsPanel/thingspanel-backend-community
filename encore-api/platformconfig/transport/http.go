// Package transport is the thin legacy HTTP adapter for the native dictionary
// owner. Business rules and SQL remain in app and store, respectively.
package transport

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"encore.app/platformconfig/app"
)

type IdentityResolver func(*http.Request) (app.Principal, error)
type MessageResolver func(int, string) string

type Handler struct {
	application *app.Service
	identity    IdentityResolver
	message     MessageResolver
}

func New(application *app.Service, identity IdentityResolver, message MessageResolver) *Handler {
	return &Handler{application: application, identity: identity, message: message}
}

type response struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (h *Handler) write(w http.ResponseWriter, r *http.Request, status, code int, data any) {
	message := "dictionary service unavailable"
	if h != nil && h.message != nil {
		message = h.message(code, r.Header.Get("Accept-Language"))
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(response{Code: code, Message: message, Data: data})
}

func (h *Handler) failure(w http.ResponseWriter, r *http.Request, err error) {
	code := 100000
	var data any
	switch {
	case errors.Is(err, app.ErrOutcomeUnknown):
		code = 101001
		data = map[string]any{"outcome": "unknown", "automatic_retry": false}
	case errors.Is(err, app.ErrUnauthorized):
		code = 201001
	case errors.Is(err, app.ErrInvalid), errors.Is(err, app.ErrNotFound):
		code = 100002
	case errors.Is(err, app.ErrConflict):
		code = 201002
	case errors.Is(err, app.ErrWriteNotApplied):
		code = 101001
	case errors.Is(err, app.ErrUnavailable), errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		code = 100001
	default:
		code = 101001
	}
	// Preserve the legacy business-error envelope and HTTP 200. Database SQL,
	// raw dependency responses and credentials are never reflected in messages.
	h.write(w, r, http.StatusOK, code, data)
}

func optionalQuery(query url.Values, key string) *string {
	if _, exists := query[key]; !exists {
		return nil
	}
	value := query.Get(key)
	return &value
}

func body(w http.ResponseWriter, r *http.Request, value any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	decoder := json.NewDecoder(r.Body)
	// Legacy JSON binding ignored unknown fields. They still cannot set any
	// trusted identity because the DTO has no identity fields.
	if err := decoder.Decode(value); err != nil {
		return app.ErrInvalid
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return app.ErrInvalid
	}
	return nil
}

func matchRoute(r *http.Request, operation app.Operation) (string, bool) {
	method, path := r.Method, r.URL.Path
	switch operation {
	case app.CreateColumn:
		return "", method == http.MethodPost && path == "/api/v1/dict/column"
	case app.CreateLanguage:
		return "", method == http.MethodPost && path == "/api/v1/dict/language"
	case app.ReadEnum:
		return "", method == http.MethodGet && path == "/api/v1/dict/enum"
	case app.ReadPage:
		return "", method == http.MethodGet && path == "/api/v1/dict"
	case app.ReadProtocols:
		return "", method == http.MethodGet && path == "/api/v1/dict/protocol/service"
	case app.ReadLanguages, app.DeleteLanguage:
		want := http.MethodGet
		if operation == app.DeleteLanguage {
			want = http.MethodDelete
		}
		prefix := "/api/v1/dict/language/"
		if method != want || !strings.HasPrefix(path, prefix) {
			return "", false
		}
		id := strings.TrimPrefix(path, prefix)
		return id, id != "" && !strings.Contains(id, "/")
	case app.DeleteColumn:
		prefix := "/api/v1/dict/column/"
		if method != http.MethodDelete || !strings.HasPrefix(path, prefix) {
			return "", false
		}
		id := strings.TrimPrefix(path, prefix)
		return id, id != "" && !strings.Contains(id, "/")
	default:
		return "", false
	}
}

func (h *Handler) Serve(operation app.Operation, w http.ResponseWriter, r *http.Request) {
	if h == nil || h.application == nil || h.identity == nil || h.message == nil {
		h.write(w, r, http.StatusServiceUnavailable, 100001, nil)
		return
	}
	principal, err := h.identity(r)
	if err != nil {
		h.failure(w, r, err)
		return
	}
	id, valid := matchRoute(r, operation)
	if !valid {
		h.failure(w, r, app.ErrInvalid)
		return
	}
	ctx := r.Context()
	query := r.URL.Query()
	var data any
	switch operation {
	case app.CreateColumn:
		var input app.CreateColumnInput
		if err = body(w, r, &input); err == nil {
			err = h.application.CreateDictionary(ctx, principal, input)
		}
	case app.CreateLanguage:
		var input app.CreateLanguageInput
		if err = body(w, r, &input); err == nil {
			err = h.application.CreateTranslation(ctx, principal, input)
		}
	case app.DeleteColumn:
		err = h.application.DeleteDictionary(ctx, principal, id)
	case app.DeleteLanguage:
		err = h.application.DeleteTranslation(ctx, principal, id)
	case app.ReadEnum:
		data, err = h.application.Enum(ctx, principal, app.EnumInput{DictCode: query.Get("dict_code"), LanguageCode: optionalQuery(query, "language_code"), AcceptLanguage: r.Header.Get("Accept-Language")})
	case app.ReadPage:
		page, pageErr := strconv.Atoi(query.Get("page"))
		size, sizeErr := strconv.Atoi(query.Get("page_size"))
		if pageErr != nil || sizeErr != nil {
			err = app.ErrInvalid
		} else {
			data, err = h.application.Page(ctx, principal, app.PageInput{Page: page, PageSize: size, DictCode: optionalQuery(query, "dict_code")})
		}
	case app.ReadLanguages:
		data, err = h.application.Languages(ctx, principal, id)
	case app.ReadProtocols:
		data, err = h.application.Protocols(ctx, principal, optionalQuery(query, "language_code"))
	default:
		err = app.ErrInvalid
	}
	if err != nil {
		h.failure(w, r, err)
		return
	}
	h.write(w, r, http.StatusOK, 200, data)
}

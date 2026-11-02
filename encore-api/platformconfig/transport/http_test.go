package transport

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"encore.app/platformconfig/app"
)

type fixtureRepository struct {
	calls    int
	err      error
	language string
	ctx      context.Context
	empty    bool
}

func (r *fixtureRepository) call(ctx context.Context) error { r.calls++; r.ctx = ctx; return r.err }
func (r *fixtureRepository) CreateDictionary(ctx context.Context, _ app.Dictionary) error {
	return r.call(ctx)
}
func (r *fixtureRepository) CreateLanguage(ctx context.Context, _ app.Language) error {
	return r.call(ctx)
}
func (r *fixtureRepository) DeleteDictionary(ctx context.Context, _ string) error { return r.call(ctx) }
func (r *fixtureRepository) DeleteLanguage(ctx context.Context, _ string) error   { return r.call(ctx) }
func (r *fixtureRepository) Enum(ctx context.Context, _, language string) ([]app.EnumValue, error) {
	r.language = language
	if err := r.call(ctx); err != nil {
		return nil, err
	}
	if r.empty {
		return nil, nil
	}
	return []app.EnumValue{{DictValue: "value", Translation: ""}}, nil
}
func (r *fixtureRepository) Page(ctx context.Context, _ app.PageInput) (app.Page, error) {
	return app.Page{Total: 0, List: []app.Dictionary{}}, r.call(ctx)
}
func (r *fixtureRepository) Languages(ctx context.Context, _ string) ([]app.Language, error) {
	return []app.Language{}, r.call(ctx)
}
func (r *fixtureRepository) Protocols(ctx context.Context, language string) ([]app.ProtocolRow, error) {
	r.language = language
	return []app.ProtocolRow{{Code: "GATEWAY_PROTOCOL", Value: app.EnumValue{DictValue: "MQTT", Translation: "gateway"}}}, r.call(ctx)
}

type fixtureAuthorization struct {
	err       error
	principal app.Principal
	operation app.Operation
}

func (a *fixtureAuthorization) Authorize(_ context.Context, p app.Principal, op app.Operation) error {
	a.principal = p
	a.operation = op
	return a.err
}

func fixtureHandler(repository *fixtureRepository, authorization *fixtureAuthorization) *Handler {
	application := app.New(repository, authorization, func() string { return "generated-id" }, time.Now)
	return New(application, func(*http.Request) (app.Principal, error) {
		return app.Principal{SubjectID: "authenticated-subject", TenantID: "authenticated-tenant", Roles: []string{"SYS_ADMIN"}}, nil
	}, func(code int, language string) string {
		if language == "en_US" {
			return "English message"
		}
		return "中文消息"
	})
}

func TestDictionaryHTTPAllEightLegacyRoutes(t *testing.T) {
	for _, test := range []struct {
		op                 app.Operation
		method, path, body string
		hasData            bool
	}{
		{app.CreateColumn, "POST", "/api/v1/dict/column", `{"dict_code":"code","dict_value":"value","tenant_id":"forged","subjectId":"forged"}`, false},
		{app.CreateLanguage, "POST", "/api/v1/dict/language", `{"dict_id":"old-id","language_code":"en_US","translation":"text"}`, false},
		{app.ReadEnum, "GET", "/api/v1/dict/enum?dict_code=code&language_code=zh_CN", "", true},
		{app.ReadPage, "GET", "/api/v1/dict?page=1&page_size=10", "", true},
		{app.ReadLanguages, "GET", "/api/v1/dict/language/old-id", "", true},
		{app.DeleteColumn, "DELETE", "/api/v1/dict/column/old-id", "", false},
		{app.DeleteLanguage, "DELETE", "/api/v1/dict/language/old-language", "", false},
		{app.ReadProtocols, "GET", "/api/v1/dict/protocol/service", "", true},
	} {
		t.Run(string(test.op), func(t *testing.T) {
			repository := &fixtureRepository{}
			authorization := &fixtureAuthorization{}
			handler := fixtureHandler(repository, authorization)
			request := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
			request.Header.Set("Accept-Language", "en_US")
			response := httptest.NewRecorder()
			handler.Serve(test.op, response, request)
			var wire map[string]json.RawMessage
			if err := json.Unmarshal(response.Body.Bytes(), &wire); err != nil {
				t.Fatal(err)
			}
			if response.Code != 200 || string(wire["code"]) != "200" || string(wire["message"]) != `"English message"` || response.Header().Get("Content-Type") != "application/json; charset=utf-8" || response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("legacy response=%d %s", response.Code, response.Body.String())
			}
			_, hasData := wire["data"]
			if hasData != test.hasData {
				t.Fatalf("data omission contract changed: %s", response.Body.String())
			}
			if repository.calls != 1 || authorization.operation != test.op || authorization.principal.SubjectID != "authenticated-subject" || authorization.principal.TenantID != "authenticated-tenant" {
				t.Fatal("unverified request identity or wrong use case")
			}
			if repository.ctx != request.Context() {
				t.Fatal("request cancellation context was replaced")
			}
			if test.op == app.ReadEnum && repository.language != "en_US" {
				t.Fatalf("enum query overrode Accept-Language: %s", repository.language)
			}
			if test.op == app.ReadProtocols && (repository.language != "zh" || !strings.Contains(string(wire["data"]), `"device_type":"2"`) || !strings.Contains(string(wire["data"]), `"device_type":"3"`)) {
				t.Fatalf("protocol compatibility=%s", wire["data"])
			}
		})
	}
}

func TestDictionaryHTTPRevocationUnknownAndNullContracts(t *testing.T) {
	repository := &fixtureRepository{}
	authorization := &fixtureAuthorization{}
	handler := fixtureHandler(repository, authorization)
	request := func(op app.Operation, method, path, body string) *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		handler.Serve(op, response, httptest.NewRequest(method, path, strings.NewReader(body)))
		return response
	}
	if response := request(app.ReadEnum, "GET", "/api/v1/dict/enum?dict_code=code", ""); !strings.Contains(response.Body.String(), `"code":200`) {
		t.Fatal(response.Body.String())
	}
	authorization.err = app.ErrUnauthorized
	if response := request(app.ReadEnum, "GET", "/api/v1/dict/enum?dict_code=code", ""); response.Code != 200 || !strings.Contains(response.Body.String(), `"code":201001`) || repository.calls != 1 {
		t.Fatal("revoked permission reached repository: ", response.Body.String())
	}
	authorization.err = nil
	repository.err = errors.Join(app.ErrOutcomeUnknown, errors.New("secret SQL must not be reflected"))
	response := request(app.CreateColumn, "POST", "/api/v1/dict/column", `{"dict_code":"c","dict_value":"v"}`)
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"code":101001`) || !strings.Contains(response.Body.String(), `"outcome":"unknown"`) || strings.Contains(response.Body.String(), "secret SQL") {
		t.Fatal("unknown became success/leak: ", response.Body.String())
	}
	repository.err = nil
	repository.empty = true
	response = request(app.ReadEnum, "GET", "/api/v1/dict/enum?dict_code=code", "")
	if !strings.Contains(response.Body.String(), `"data":null`) {
		t.Fatal("legacy empty enum changed: ", response.Body.String())
	}
	before := repository.calls
	response = request(app.ReadPage, "GET", "/api/v1/dict?page=0&page_size=10", "")
	if !strings.Contains(response.Body.String(), `"code":100002`) || repository.calls != before {
		t.Fatal("invalid page reached repository")
	}
	response = request(app.CreateColumn, "POST", "/api/v1/dict/column", `{"dict_code":"c","dict_value":"v"} {}`)
	if !strings.Contains(response.Body.String(), `"code":100002`) || repository.calls != before {
		t.Fatal("trailing JSON reached repository")
	}
	var disabled *Handler
	out := httptest.NewRecorder()
	disabled.Serve(app.ReadPage, out, httptest.NewRequest("GET", "/api/v1/dict?page=1&page_size=10", nil))
	if out.Code != 503 {
		t.Fatal("disabled owner did not fail closed")
	}
}

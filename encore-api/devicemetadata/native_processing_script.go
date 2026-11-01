package devicemetadata

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"unicode/utf8"

	"encore.app/internal/authz"
	"encore.app/internal/luasandbox"
	"encore.app/internal/nativehttp"
	"encore.app/l1-access/authn"
	"encore.dev/beta/errs"
)

// Debug runs of a template's Lua processing scripts (DM-TPL-018). The run is
// isolated and has no downlink side effect, so it needs no idempotency record.

const maxProcessingScriptTestBody = 1 << 20

type processingScriptTestScript struct {
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	Description     *string `json:"description"`
	Phase           string  `json:"phase"`
	Language        string  `json:"language"`
	Entrypoint      string  `json:"entrypoint"`
	Content         string  `json:"content"`
	Enabled         bool    `json:"enabled"`
	LastSampleInput *string `json:"lastSampleInput"`
}

type processingScriptTestRequest struct {
	Script     processingScriptTestScript `json:"script"`
	Input      *string                    `json:"input"`
	Topic      *string                    `json:"topic"`
	TemplateID *string                    `json:"templateId"`
}

type processingScriptTestRequestView struct {
	Script     processingScriptTestScript
	Input      string
	Topic      string
	TemplateID *string
}

type ProcessingScriptTestResult struct {
	Succeeded     bool    `json:"succeeded"`
	Output        *string `json:"output"`
	ErrorCode     *string `json:"errorCode"`
	Message       *string `json:"message"`
	DurationMs    int64   `json:"durationMs"`
	EngineVersion string  `json:"engineVersion"`
}

type ProcessingScriptTestResultResponse struct {
	Data ProcessingScriptTestResult `json:"data"`
	Meta RequestMeta                `json:"meta"`
}

func decodeProcessingScriptTest(body []byte) (processingScriptTestRequestView, error) {
	invalid := func(message string) error {
		return &errs.Error{Code: errs.InvalidArgument, Message: message}
	}
	var raw processingScriptTestRequest
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&raw); err != nil {
		return processingScriptTestRequestView{}, invalid("request body is not a valid processing script test")
	}
	if _, err := decoder.Token(); err != io.EOF {
		return processingScriptTestRequestView{}, invalid("request body has trailing data")
	}
	if raw.Input == nil || raw.Topic == nil {
		return processingScriptTestRequestView{}, invalid("input and topic are required")
	}
	script := raw.Script
	switch {
	case script.Language != "lua":
		return processingScriptTestRequestView{}, invalid("script.language must be lua")
	case script.Entrypoint != luasandbox.Entrypoint:
		return processingScriptTestRequestView{}, invalid("script.entrypoint must be " + luasandbox.Entrypoint)
	case script.Content == "" || len(script.Content) > 262144 || !utf8.ValidString(script.Content):
		return processingScriptTestRequestView{}, invalid("script.content must be 1 to 262144 bytes of UTF-8")
	case len(*raw.Input) > 262144 || len(*raw.Topic) > 1024:
		return processingScriptTestRequestView{}, invalid("input or topic is too long")
	}
	if raw.TemplateID != nil {
		if _, err := nativeReadUUID(*raw.TemplateID); err != nil {
			return processingScriptTestRequestView{}, invalid("templateId must be a UUID or null")
		}
	}
	return processingScriptTestRequestView{Script: script, Input: *raw.Input, Topic: *raw.Topic, TemplateID: raw.TemplateID}, nil
}

func runProcessingScriptTest(ctx context.Context, req processingScriptTestRequestView) ProcessingScriptTestResult {
	run := luasandbox.Run(ctx, req.Script.Content, req.Input, req.Topic)
	result := ProcessingScriptTestResult{
		Succeeded:     run.Succeeded,
		DurationMs:    run.Duration.Milliseconds(),
		EngineVersion: luasandbox.EngineVersion,
	}
	if run.Succeeded {
		result.Output = &run.Output
		return result
	}
	result.ErrorCode, result.Message = &run.ErrorCode, &run.Message
	return result
}

//encore:api auth raw method=POST path=/api/v2/processing-script-tests
func (s *Service) TestNativeProcessingScript(w http.ResponseWriter, r *http.Request) {
	requestID := newID()
	data := authn.CurrentData()
	if data == nil || data.TenantID == "" {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, &errs.Error{Code: errs.Unauthenticated, Message: "authentication required"}, 0, true)
		return
	}
	if !authz.Can(data, "template", "diagnose") {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, &errs.Error{Code: errs.PermissionDenied, Message: "role is not authorized to run template scripts"}, 0, true)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxProcessingScriptTestBody))
	if err != nil {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, &errs.Error{Code: errs.InvalidArgument, Message: "request body is missing or too large"}, 0, true)
		return
	}
	request, err := decodeProcessingScriptTest(body)
	if err != nil {
		nativehttp.WriteProblem(w, requestID, r.URL.Path, err, 0, true)
		return
	}
	response := ProcessingScriptTestResultResponse{Data: runProcessingScriptTest(r.Context(), request), Meta: RequestMeta{RequestID: requestID}}
	w.Header().Set("Cache-Control", "no-store")
	nativehttp.WriteJSON(w, requestID, r.URL.Path, http.StatusOK, "", response)
}

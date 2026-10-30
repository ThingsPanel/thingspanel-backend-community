// Package nativehttp provides the common native metadata response contract.
package nativehttp

import (
	"encoding/json"
	"errors"
	"net/http"

	"encore.dev/beta/errs"
)

type RequestMeta struct {
	RequestID string `json:"requestId"`
}

type Problem struct {
	Type      string `json:"type"`
	Title     string `json:"title"`
	Status    int    `json:"status"`
	Detail    string `json:"detail"`
	Instance  string `json:"instance"`
	Code      string `json:"code"`
	RequestID string `json:"requestId"`
}

func WriteJSON(w http.ResponseWriter, requestID, instance string, status int, etag string, payload any) {
	body, err := json.Marshal(payload)
	if err != nil {
		WriteProblem(w, requestID, instance, &errs.Error{Code: errs.Internal, Message: "failed to encode response"}, 0, false)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Request-Id", requestID)
	if etag != "" {
		w.Header().Set("ETag", etag)
	}
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func WriteProblem(w http.ResponseWriter, requestID, instance string, err error, statusOverride int, invalidAs422 bool) {
	var apiErr *errs.Error
	if !errors.As(err, &apiErr) {
		apiErr = &errs.Error{Code: errs.Internal, Message: "internal server error"}
	}
	status := apiErr.Code.HTTPStatus()
	if statusOverride != 0 {
		status = statusOverride
	} else if invalidAs422 && apiErr.Code == errs.InvalidArgument {
		status = http.StatusUnprocessableEntity
	}
	if status < 400 || status > 599 {
		status = http.StatusInternalServerError
	}
	detail := apiErr.Message
	if status >= 500 {
		detail = "internal server error"
	}
	body, marshalErr := json.Marshal(Problem{
		Type: "about:blank", Title: http.StatusText(status), Status: status,
		Detail: detail, Instance: instance, Code: apiErr.Code.String(), RequestID: requestID,
	})
	if marshalErr != nil {
		status = http.StatusInternalServerError
		body, _ = json.Marshal(Problem{Type: "about:blank", Title: http.StatusText(status), Status: status,
			Detail: "internal server error", Instance: instance, Code: errs.Internal.String(), RequestID: requestID})
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("X-Request-Id", requestID)
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

package devicemetadata

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strings"

	"encore.dev"
	"encore.dev/beta/errs"

	"encore.app/devicemetadata/accessconfig"
)

type NativeValidationIssue struct {
	Pointer  string `json:"pointer"`
	Code     string `json:"code"`
	Message  string `json:"message"`
	Severity string `json:"severity"`
}

type NativeValidationReport struct {
	Valid        bool                    `json:"valid"`
	Issues       []NativeValidationIssue `json:"issues"`
	SourceDigest string                  `json:"sourceDigest"`
}

type NativeValidationReportResponse struct {
	Data NativeValidationReport `json:"data"`
	Meta RequestMeta            `json:"meta"`
}

func validationPointer(field string) string {
	field = strings.TrimPrefix(field, "$")
	field = strings.TrimPrefix(field, "configuration")
	field = strings.Trim(field, ".")
	if field == "" {
		return "/configuration"
	}
	return "/configuration/" + strings.ReplaceAll(field, ".", "/")
}

// validateNativeTemplateConfigurationReport checks a configuration exactly as a
// save would, but stores nothing. A configuration that is wrong is a report with
// issues, not an HTTP error; only an unavailable dependency is an error.
func validateNativeTemplateConfigurationReport(ctx context.Context, identity nativeTemplateWriteIdentity, templateID string, body []byte, check nativeDeviceConfigurationCheck) (*NativeValidationReport, error) {
	digest := sha256.Sum256(body)
	report := &NativeValidationReport{Issues: []NativeValidationIssue{}, SourceDigest: hex.EncodeToString(digest[:])}
	tx, err := beginTenantScopedSQLTx(ctx, identity.tenantID, true)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// Validating against a template the caller cannot see answers NotFound.
	if _, err := loadNativeDeviceAccessTemplateTx(ctx, tx, identity.tenantID, templateID, identity.deviceIDs); err != nil {
		return nil, err
	}
	add := func(pointer, code, message string) {
		report.Issues = append(report.Issues, NativeValidationIssue{Pointer: pointer, Code: code, Message: message, Severity: "error"})
	}
	configuration, err := accessconfig.DecodeConfiguration(body)
	if err != nil {
		add("/configuration", "invalid_configuration", "configuration is not a valid template configuration")
		return report, nil
	}
	if err := validateNativeTemplateConfigurationTx(ctx, tx, identity.tenantID, configuration, check); err != nil {
		var inputErr *NativeDeviceInputError
		var apiErr *errs.Error
		switch {
		case errors.As(err, &inputErr):
			add(validationPointer(inputErr.Field), "invalid_value", inputErr.Reason)
		case errors.As(err, &apiErr) && (apiErr.Code == errs.InvalidArgument || apiErr.Code == errs.NotFound || apiErr.Code == errs.FailedPrecondition):
			add("/configuration", "invalid_value", apiErr.Message)
		default:
			return nil, err
		}
	}
	report.Valid = len(report.Issues) == 0
	return report, nil
}

//encore:api auth raw method=POST path=/api/v2/device-access-templates/:templateID/validations
func (s *Service) ValidateNativeDeviceAccessTemplate(w http.ResponseWriter, r *http.Request) {
	requestID := newID()
	identity, err := currentNativeTemplateWriteIdentity()
	if err != nil {
		writeGroupReadProblem(w, requestID, r.URL.Path, err)
		return
	}
	if _, err := parseNativeReadValues(r, nil); err != nil {
		writeGroupCreateProblem(w, requestID, r.URL.Path, err, http.StatusBadRequest)
		return
	}
	id, err := nativeReadUUID(encore.CurrentRequest().PathParams.Get("templateID"))
	if err != nil {
		writeGroupCreateProblem(w, requestID, r.URL.Path, err, http.StatusBadRequest)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, nativeTemplateWriteMaxBytes))
	if err != nil {
		writeGroupCreateProblem(w, requestID, r.URL.Path, &errs.Error{Code: errs.InvalidArgument, Message: "template request body is too large or unreadable"}, http.StatusBadRequest)
		return
	}
	report, err := validateNativeTemplateConfigurationReport(r.Context(), identity, id, body, validateNativeDeviceConfiguration)
	if err != nil {
		writeNativeTemplateReadProblem(w, r, requestID, err)
		return
	}
	writeGroupReadJSON(w, requestID, r.URL.Path, http.StatusOK, "", NativeValidationReportResponse{Data: *report, Meta: RequestMeta{RequestID: requestID}})
}

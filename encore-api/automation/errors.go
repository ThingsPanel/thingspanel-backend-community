package automation

import "encore.dev/beta/errs"

// ErrorDetail is the client-visible half of a failure.
//
// Encore returns Details to callers and keeps Meta inside the process. The
// first version of these errors wrote the code into Meta, which meant the web
// page and the CLI received the sentence but never the code: a caller could see
// "预览已过期" and still have no way to branch on it without parsing Chinese.
// A26 is what surfaced it — a command line that must decide an exit code is the
// first consumer that cannot paper over a missing code.
//
// Both fields are set: Details is the contract, Meta stays for in-process
// assertions and for internal callers that read the error before it is
// serialised.
type ErrorDetail struct {
	// Code is our own vocabulary: invalid_definition, preview_stale,
	// binding_incompatible, role_unmapped, directory_unavailable, …
	Code string `json:"code,omitempty"`
	// Field names the single input that was wrong, when there is one.
	Field string `json:"field,omitempty"`
	// Fields carries field-level issues for a validation failure, so a form can
	// mark the right inputs instead of printing one long sentence.
	Fields []map[string]string `json:"fields,omitempty"`
	// RuleID / ImportID / Status identify the object the failure is about.
	RuleID   string `json:"rule_id,omitempty"`
	ImportID string `json:"import_id,omitempty"`
	Status   string `json:"status,omitempty"`
	// ReasonCode is the machine code of a failed import.
	ReasonCode string `json:"reason_code,omitempty"`
	// ReferencingRuleIDs lists who still points at the object being deleted.
	ReferencingRuleIDs []string `json:"referencing_rule_ids,omitempty"`
}

func (ErrorDetail) ErrDetails() {}

// detailError builds an error that both a program and a person can read.
func detailError(code errs.ErrCode, msg string, d ErrorDetail) *errs.Error {
	meta := errs.Metadata{}
	if d.Code != "" {
		meta["code"] = d.Code
	}
	if d.Field != "" {
		meta["field"] = d.Field
	}
	if len(d.Fields) > 0 {
		meta["fields"] = d.Fields
	}
	if d.RuleID != "" {
		meta["rule_id"] = d.RuleID
	}
	if d.ImportID != "" {
		meta["import_id"] = d.ImportID
	}
	if d.Status != "" {
		meta["status"] = d.Status
	}
	if d.ReasonCode != "" {
		meta["reason_code"] = d.ReasonCode
	}
	if len(d.ReferencingRuleIDs) > 0 {
		meta["referencing_rule_ids"] = d.ReferencingRuleIDs
	}
	return &errs.Error{Code: code, Message: msg, Details: d, Meta: meta}
}

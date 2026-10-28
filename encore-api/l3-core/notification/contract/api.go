// Package contract contains the stable JSON DTOs shared by the notification
// service boundary and its contract tests. Persistence models stay separate.
package contract

// Envelope is the successful business v2 response envelope. The HTTP status
// code remains independent; create and accepted operations use 201/202 while
// Code remains 200 as required by the public contract.
type Envelope[T any] struct {
	Code      int    `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"requestId"`
	Data      T      `json:"data"`
}

// DomainError is returned for notification domain failures. Encore runtime
// authentication/decoding failures may use its own error representation.
type DomainError struct {
	Code      int         `json:"code"`
	Message   string      `json:"message"`
	Details   ErrorDetail `json:"details"`
	RequestID string      `json:"requestId"`
}

type ErrorDetail struct {
	Reason string       `json:"reason"`
	Fields []FieldError `json:"fields,omitempty"`
}

type FieldError struct {
	Field  string `json:"field"`
	Reason string `json:"reason"`
}

// ReceiptAck is a protocol-level response, not a business envelope.
type ReceiptAck struct {
	Accepted bool `json:"accepted"`
}

// AcceptedData means the notification intent was committed for durable
// processing. It does not mean a provider accepted or delivered it.
type AcceptedData struct {
	Accepted       bool     `json:"accepted"`
	NotificationID string   `json:"notificationId"`
	DeliveryIDs    []string `json:"deliveryIds"`
	IntakeStatus   string   `json:"intakeStatus"`
	BlockedReason  string   `json:"blockedReason,omitempty"`
}

type NotificationListData struct {
	Items    []NotificationView `json:"items"`
	Page     int                `json:"page"`
	PageSize int                `json:"pageSize"`
	Total    int64              `json:"total"`
}

type PluginGrantPage struct {
	Items    []PluginGrantView `json:"items"`
	Page     int               `json:"page"`
	PageSize int               `json:"pageSize"`
	Total    int64             `json:"total"`
}

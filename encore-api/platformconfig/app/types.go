// Package app owns the platform dictionary use cases. It has no HTTP, Encore,
// Gin, global connection, or legacy module dependency.
package app

import (
	"context"
	"errors"
	"time"
)

var (
	ErrInvalid         = errors.New("invalid dictionary request")
	ErrUnauthorized    = errors.New("dictionary permission denied")
	ErrUnavailable     = errors.New("dictionary dependency unavailable")
	ErrNotFound        = errors.New("dictionary parent not found")
	ErrConflict        = errors.New("dictionary value conflicts")
	ErrWriteNotApplied = errors.New("dictionary write was not applied exactly once")
	ErrOutcomeUnknown  = errors.New("dictionary transaction outcome is unknown")
)

type Operation string

const (
	CreateColumn   Operation = "create-column"
	CreateLanguage Operation = "create-language"
	ReadEnum       Operation = "read-enum"
	ReadPage       Operation = "read-page"
	ReadLanguages  Operation = "read-languages"
	DeleteColumn   Operation = "delete-column"
	DeleteLanguage Operation = "delete-language"
	ReadProtocols  Operation = "read-protocols"
)

// Principal is supplied only by the authenticated host, never by a JSON body.
// The authorizer independently checks current account and resource permission.
type Principal struct {
	SubjectID string
	TenantID  string
	Roles     []string
}

type Authorizer interface {
	Authorize(context.Context, Principal, Operation) error
}

type Dictionary struct {
	ID        string    `json:"id"`
	DictCode  string    `json:"dict_code"`
	DictValue string    `json:"dict_value"`
	CreatedAt time.Time `json:"created_at"`
	Remark    *string   `json:"remark"`
}

type Language struct {
	ID           string `json:"id"`
	DictID       string `json:"dict_id"`
	LanguageCode string `json:"language_code"`
	Translation  string `json:"translation"`
}

type CreateColumnInput struct {
	DictCode  string  `json:"dict_code"`
	DictValue string  `json:"dict_value"`
	Remark    *string `json:"remark"`
}

type CreateLanguageInput struct {
	DictID       string `json:"dict_id"`
	LanguageCode string `json:"language_code"`
	Translation  string `json:"translation"`
}

type EnumInput struct {
	DictCode       string
	LanguageCode   *string // legacy input is validated but Accept-Language selects translation
	AcceptLanguage string
}

type PageInput struct {
	Page     int
	PageSize int
	DictCode *string
}

type Page struct {
	Total int64        `json:"total"`
	List  []Dictionary `json:"list"`
}

type EnumValue struct {
	DictValue   string `json:"dict_value"`
	Translation string `json:"translation"`
}

type ProtocolValue struct {
	DictValue   string `json:"dict_value"`
	Translation string `json:"translation"`
	DeviceType  string `json:"device_type"`
}

type ProtocolRow struct {
	Code  string
	Value EnumValue
}

// Repository is the only dictionary data access port. Its implementation owns
// the parent/child locks, affected-row checks and transaction outcomes.
type Repository interface {
	CreateDictionary(context.Context, Dictionary) error
	CreateLanguage(context.Context, Language) error
	DeleteDictionary(context.Context, string) error
	DeleteLanguage(context.Context, string) error
	Enum(context.Context, string, string) ([]EnumValue, error)
	Page(context.Context, PageInput) (Page, error)
	Languages(context.Context, string) ([]Language, error)
	Protocols(context.Context, string) ([]ProtocolRow, error)
}

package app

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"
)

type Service struct {
	repository Repository
	authorizer Authorizer
	newID      func() string
	now        func() time.Time
}

func New(repository Repository, authorizer Authorizer, newID func() string, now func() time.Time) *Service {
	return &Service{repository: repository, authorizer: authorizer, newID: newID, now: now}
}

func RequiresAdministrator(operation Operation) bool {
	switch operation {
	case CreateColumn, CreateLanguage, DeleteColumn, DeleteLanguage, ReadPage:
		return true
	default:
		return false
	}
}

func ValidOperation(operation Operation) bool {
	switch operation {
	case CreateColumn, CreateLanguage, DeleteColumn, DeleteLanguage, ReadEnum, ReadPage, ReadLanguages, ReadProtocols:
		return true
	default:
		return false
	}
}

func IsPlatformAdministrator(principal Principal) bool {
	for _, role := range principal.Roles {
		switch strings.ToUpper(role) {
		case "SYS_ADMIN", "PLATFORM_ADMIN":
			return true
		}
	}
	return false
}

// CompleteIdentity follows the existing authcore admission rule: a current
// SYS_ADMIN (native platform_admin) may have no tenant. This is only identity
// shape validation; every operation still calls the current policy owner.
func CompleteIdentity(principal Principal) bool {
	if strings.TrimSpace(principal.SubjectID) == "" {
		return false
	}
	if principal.TenantID == "" {
		return IsPlatformAdministrator(principal)
	}
	return strings.TrimSpace(principal.TenantID) != ""
}

func (s *Service) authorize(ctx context.Context, principal Principal, operation Operation) error {
	if ctx == nil || !ValidOperation(operation) {
		return ErrInvalid
	}
	if !CompleteIdentity(principal) {
		return ErrUnauthorized
	}
	if s == nil || s.repository == nil || s.authorizer == nil || s.newID == nil || s.now == nil {
		return ErrUnavailable
	}
	if RequiresAdministrator(operation) {
		if !IsPlatformAdministrator(principal) {
			return ErrUnauthorized
		}
	}
	return s.authorizer.Authorize(ctx, principal, operation)
}

func validString(value string, max int, required bool) bool {
	return (!required || value != "") && utf8.ValidString(value) && utf8.RuneCountInString(value) <= max && !strings.ContainsRune(value, '\x00')
}

func validOptional(value *string, max int) bool {
	return value == nil || validString(*value, max, false)
}

func validID(value string) bool {
	return validString(value, 36, true) && !strings.Contains(value, "/")
}

func (s *Service) CreateDictionary(ctx context.Context, principal Principal, input CreateColumnInput) error {
	if err := s.authorize(ctx, principal, CreateColumn); err != nil {
		return err
	}
	if !validString(input.DictCode, 36, true) || !validString(input.DictValue, 255, true) || !validOptional(input.Remark, 255) {
		return ErrInvalid
	}
	id := s.newID()
	if !validID(id) {
		return ErrUnavailable
	}
	var remark *string
	if input.Remark != nil {
		copy := *input.Remark
		remark = &copy
	}
	// PG's retained legacy schema stores microsecond timestamps.
	row := Dictionary{ID: id, DictCode: input.DictCode, DictValue: input.DictValue, Remark: remark, CreatedAt: s.now().UTC().Truncate(time.Microsecond)}
	return s.repository.CreateDictionary(ctx, row)
}

func (s *Service) CreateTranslation(ctx context.Context, principal Principal, input CreateLanguageInput) error {
	if err := s.authorize(ctx, principal, CreateLanguage); err != nil {
		return err
	}
	if !validID(input.DictID) || !validString(input.LanguageCode, 36, true) || !validString(input.Translation, 255, true) {
		return ErrInvalid
	}
	id := s.newID()
	if !validID(id) {
		return ErrUnavailable
	}
	return s.repository.CreateLanguage(ctx, Language{ID: id, DictID: input.DictID, LanguageCode: input.LanguageCode, Translation: input.Translation})
}

func (s *Service) DeleteDictionary(ctx context.Context, principal Principal, id string) error {
	if err := s.authorize(ctx, principal, DeleteColumn); err != nil {
		return err
	}
	if !validID(id) {
		return ErrInvalid
	}
	return s.repository.DeleteDictionary(ctx, id)
}

func (s *Service) DeleteTranslation(ctx context.Context, principal Principal, id string) error {
	if err := s.authorize(ctx, principal, DeleteLanguage); err != nil {
		return err
	}
	if !validID(id) {
		return ErrInvalid
	}
	return s.repository.DeleteLanguage(ctx, id)
}

func (s *Service) Enum(ctx context.Context, principal Principal, input EnumInput) ([]EnumValue, error) {
	if err := s.authorize(ctx, principal, ReadEnum); err != nil {
		return nil, err
	}
	if !validString(input.DictCode, 36, true) || !validOptional(input.LanguageCode, 36) {
		return nil, ErrInvalid
	}
	return s.repository.Enum(ctx, input.DictCode, FormatLangCode(input.AcceptLanguage))
}

func (s *Service) Page(ctx context.Context, principal Principal, input PageInput) (Page, error) {
	if err := s.authorize(ctx, principal, ReadPage); err != nil {
		return Page{}, err
	}
	if input.Page < 1 || input.PageSize < 1 || input.PageSize > 1000 || input.Page-1 > int(^uint(0)>>1)/input.PageSize || !validOptional(input.DictCode, 36) {
		return Page{}, ErrInvalid
	}
	return s.repository.Page(ctx, input)
}

func (s *Service) Languages(ctx context.Context, principal Principal, id string) ([]Language, error) {
	if err := s.authorize(ctx, principal, ReadLanguages); err != nil {
		return nil, err
	}
	if !validID(id) {
		return nil, ErrInvalid
	}
	return s.repository.Languages(ctx, id)
}

func (s *Service) Protocols(ctx context.Context, principal Principal, languageCode *string) ([]ProtocolValue, error) {
	if err := s.authorize(ctx, principal, ReadProtocols); err != nil {
		return nil, err
	}
	if !validOptional(languageCode, 36) {
		return nil, ErrInvalid
	}
	language := "zh"
	if languageCode != nil {
		language = *languageCode
	}
	rows, err := s.repository.Protocols(ctx, language)
	if err != nil {
		return nil, err
	}
	var result []ProtocolValue
	for _, group := range []struct{ code, deviceType string }{{"DRIECT_ATTACHED_PROTOCOL", "1"}, {"GATEWAY_PROTOCOL", "2"}, {"GATEWAY_PROTOCOL", "3"}} {
		for _, row := range rows {
			if row.Code == group.code {
				result = append(result, ProtocolValue{DictValue: row.Value.DictValue, Translation: row.Value.Translation, DeviceType: group.deviceType})
			}
		}
	}
	return result, nil
}

// FormatLangCode is the moved dictionary language rule. It deliberately keeps
// the legacy empty-header default (en_US), zh/en aliases and fallback (zh_CN).
// The protocol-menu query has its separate historical default, "zh".
func FormatLangCode(acceptLanguage string) string {
	if acceptLanguage == "" {
		return "en_US"
	}
	primary := strings.TrimSpace(strings.Split(acceptLanguage, ",")[0])
	primary = strings.Split(primary, ";")[0]
	primary = strings.Replace(primary, "-", "_", 1)
	switch primary {
	case "zh":
		return "zh_CN"
	case "en":
		return "en_US"
	}
	if len(primary) == 5 && primary[2] == '_' {
		return primary
	}
	return "zh_CN"
}

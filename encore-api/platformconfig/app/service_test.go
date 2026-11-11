package app

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

type authorizationFunc func(context.Context, Principal, Operation) error

func (f authorizationFunc) Authorize(ctx context.Context, p Principal, op Operation) error {
	return f(ctx, p, op)
}

type recordingRepository struct {
	calls        int
	language     string
	protocolRows []ProtocolRow
}

func (r *recordingRepository) CreateDictionary(context.Context, Dictionary) error {
	r.calls++
	return nil
}
func (r *recordingRepository) CreateLanguage(context.Context, Language) error { r.calls++; return nil }
func (r *recordingRepository) DeleteDictionary(context.Context, string) error { r.calls++; return nil }
func (r *recordingRepository) DeleteLanguage(context.Context, string) error   { r.calls++; return nil }
func (r *recordingRepository) Enum(_ context.Context, _, language string) ([]EnumValue, error) {
	r.calls++
	r.language = language
	return nil, nil
}
func (r *recordingRepository) Page(context.Context, PageInput) (Page, error) {
	r.calls++
	return Page{}, nil
}
func (r *recordingRepository) Languages(context.Context, string) ([]Language, error) {
	r.calls++
	return nil, nil
}
func (r *recordingRepository) Protocols(_ context.Context, language string) ([]ProtocolRow, error) {
	r.calls++
	r.language = language
	return r.protocolRows, nil
}

func TestDictionaryAuthorizationBeforeEveryRepositoryEntry(t *testing.T) {
	operations := []struct {
		name Operation
		run  func(*Service, Principal) error
	}{
		{CreateColumn, func(s *Service, p Principal) error {
			return s.CreateDictionary(context.Background(), p, CreateColumnInput{DictCode: "a", DictValue: "b"})
		}},
		{CreateLanguage, func(s *Service, p Principal) error {
			return s.CreateTranslation(context.Background(), p, CreateLanguageInput{DictID: "id", LanguageCode: "en_US", Translation: "text"})
		}},
		{DeleteColumn, func(s *Service, p Principal) error { return s.DeleteDictionary(context.Background(), p, "id") }},
		{DeleteLanguage, func(s *Service, p Principal) error { return s.DeleteTranslation(context.Background(), p, "id") }},
		{ReadEnum, func(s *Service, p Principal) error {
			_, e := s.Enum(context.Background(), p, EnumInput{DictCode: "code"})
			return e
		}},
		{ReadPage, func(s *Service, p Principal) error {
			_, e := s.Page(context.Background(), p, PageInput{Page: 1, PageSize: 10})
			return e
		}},
		{ReadLanguages, func(s *Service, p Principal) error { _, e := s.Languages(context.Background(), p, "id"); return e }},
		{ReadProtocols, func(s *Service, p Principal) error { _, e := s.Protocols(context.Background(), p, nil); return e }},
	}
	admin := Principal{SubjectID: "admin", TenantID: "tenant", Roles: []string{"platform_admin"}}
	for _, op := range operations {
		t.Run(string(op.name), func(t *testing.T) {
			for _, denial := range []error{ErrUnauthorized, ErrUnavailable} {
				repository := &recordingRepository{}
				authCalls := 0
				s := New(repository, authorizationFunc(func(_ context.Context, p Principal, actual Operation) error {
					authCalls++
					if p.SubjectID != admin.SubjectID || p.TenantID != admin.TenantID || actual != op.name {
						t.Fatal("identity or action changed")
					}
					return denial
				}), func() string { return "id" }, time.Now)
				if err := op.run(s, admin); !errors.Is(err, denial) || repository.calls != 0 || authCalls != 1 {
					t.Fatalf("denial=%v err=%v sql=%d auth=%d", denial, err, repository.calls, authCalls)
				}
			}
			repository := &recordingRepository{}
			s := New(repository, authorizationFunc(func(context.Context, Principal, Operation) error { return nil }), func() string { return "id" }, time.Now)
			if err := op.run(s, Principal{}); !errors.Is(err, ErrUnauthorized) || repository.calls != 0 {
				t.Fatalf("empty identity reached repository: %v/%d", err, repository.calls)
			}
			if RequiresAdministrator(op.name) {
				if err := op.run(s, Principal{SubjectID: "tenant-admin", TenantID: "tenant", Roles: []string{"TENANT_ADMIN"}}); !errors.Is(err, ErrUnauthorized) || repository.calls != 0 {
					t.Fatalf("tenant administrator reached global write/page: %v", err)
				}
			}
		})
	}
}

func TestDictionaryLegacyLanguageAndProtocolContract(t *testing.T) {
	for _, example := range []struct{ header, expected string }{{"", "en_US"}, {"zh", "zh_CN"}, {"en", "en_US"}, {"zh-CN, en;q=0.8", "zh_CN"}, {"en-US;q=0.9", "en_US"}, {"fr-FR", "fr_FR"}, {"invalid", "zh_CN"}, {" ", "zh_CN"}} {
		if actual := FormatLangCode(example.header); actual != example.expected {
			t.Fatalf("header %q=%q, expected %q", example.header, actual, example.expected)
		}
	}
	repository := &recordingRepository{protocolRows: []ProtocolRow{{Code: "GATEWAY_PROTOCOL", Value: EnumValue{DictValue: "MQTT", Translation: "gateway"}}, {Code: "DRIECT_ATTACHED_PROTOCOL", Value: EnumValue{DictValue: "MQTT", Translation: "direct"}}}}
	s := New(repository, authorizationFunc(func(context.Context, Principal, Operation) error { return nil }), func() string { return "id" }, time.Now)
	p := Principal{SubjectID: "reader", TenantID: "tenant", Roles: []string{"TENANT_USER"}}
	ignoredQuery := "zh_CN"
	if _, err := s.Enum(context.Background(), p, EnumInput{DictCode: "code", LanguageCode: &ignoredQuery}); err != nil || repository.language != "en_US" {
		t.Fatalf("legacy enum did not use header default: %q/%v", repository.language, err)
	}
	values, err := s.Protocols(context.Background(), p, nil)
	if err != nil || repository.language != "zh" {
		t.Fatalf("protocol default=%q err=%v", repository.language, err)
	}
	expected := []ProtocolValue{{DictValue: "MQTT", Translation: "direct", DeviceType: "1"}, {DictValue: "MQTT", Translation: "gateway", DeviceType: "2"}, {DictValue: "MQTT", Translation: "gateway", DeviceType: "3"}}
	if !reflect.DeepEqual(values, expected) {
		t.Fatalf("protocol contract=%#v", values)
	}
	if ignoredQuery != "zh_CN" {
		t.Fatal("request was mutated")
	}
}

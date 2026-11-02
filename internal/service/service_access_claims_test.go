package service

import (
	"context"
	"errors"
	"testing"

	"gorm.io/gorm"
	"project/internal/model"
	"project/pkg/errcode"
	"project/pkg/utils"
)

func TestServiceAccessRejectsMissingIdentity(t *testing.T) {
	svc := &ServiceAccess{}
	for _, claims := range []*utils.UserClaims{nil, {}, {ID: "user"}, {TenantID: "tenant"}, {ID: " ", TenantID: "tenant"}} {
		checks := []error{svc.Update(&model.UpdateAccessReq{}, claims), svc.Delete("id", claims)}
		_, err := svc.List(&model.GetServiceAccessByPageReq{}, claims)
		checks = append(checks, err)
		_, err = svc.CreateAccess(&model.CreateAccessReq{Name: "new", ServicePluginID: "plugin", Voucher: "fixture"}, claims)
		checks = append(checks, err)
		_, err = svc.GetCreateOperation(context.Background(), "original-key", claims)
		checks = append(checks, err)
		for _, err := range checks {
			var e *errcode.Error
			if !errors.As(err, &e) || e.Code != errcode.CodeNoPermission {
				t.Fatalf("claims=%+v error=%v", claims, err)
			}
		}
	}
}

func TestServiceAccessLookupError(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code int
	}{{gorm.ErrRecordNotFound, errcode.CodeNotFound}, {errors.New("db unavailable"), errcode.CodeDBError}} {
		var e *errcode.Error
		if err := accessLookupError(tc.err); !errors.As(err, &e) || e.Code != tc.code {
			t.Fatalf("got %v", err)
		}
	}
}

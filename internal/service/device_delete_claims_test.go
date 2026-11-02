package service

import (
	"errors"
	"project/pkg/errcode"
	"project/pkg/utils"
	"testing"
)

func TestDeviceDeleteRejectsMissingActorOrTenant(t *testing.T) {
	for _, claims := range []*utils.UserClaims{nil, {}, {ID: "actor"}, {TenantID: "tenant"}, {ID: " ", TenantID: "tenant"}} {
		err := (&Device{}).DeleteDevice("device", claims)
		var got *errcode.Error
		if !errors.As(err, &got) || got.Code != errcode.CodeNoPermission {
			t.Fatalf("claims=%+v error=%v", claims, err)
		}
	}
}

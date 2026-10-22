package service

import (
	"context"
	"errors"
	"os"
	"project/internal/dal"
	"project/internal/model"
	"project/pkg/utils"
	"strings"
	"time"
)

func (*MessagePush) BindDevice(ctx context.Context, req *model.CreateMessagePushReq, claims *utils.UserClaims, token string) (dal.AppPushDevice, error) {
	appID := os.Getenv("APP_PUSH_APP_ID")
	if appID == "" {
		appID = "__UNI__71E31D5"
	}
	valid := func(s string, max int) bool {
		return s != "" && len(s) <= max && !strings.ContainsAny(s, " ,\t\r\n\x00/\\")
	}
	if token == "" || claims.ExpiresAt <= time.Now().Unix() || claims.TenantID == "" || req.AppID != appID || !valid(req.PushId, 256) || !valid(req.InstallationID, 128) || (req.DeviceType != "android" && req.DeviceType != "ios") {
		return dal.AppPushDevice{}, errors.New("invalid device registration")
	}
	return dal.BindAppPushDevice(ctx, dal.AppPushDevice{TenantID: claims.TenantID, UserID: claims.ID, AppID: appID, InstallationID: req.InstallationID, CID: req.PushId, Platform: req.DeviceType, ExpiresAt: time.Unix(claims.ExpiresAt, 0)}, token)
}
func (*MessagePush) UnbindDevice(ctx context.Context, user, cid, token string) error {
	return dal.UnbindAppPushDevice(ctx, user, cid, token)
}

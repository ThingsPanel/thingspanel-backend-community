package logic

import (
	"context"
	"project/constant"
	"project/query"
)

func UserIsEncrypt(ctx context.Context) bool {
	var (
		sysFunction = query.SysFunction
	)
	// 默认没有该配置为关闭状态
	info, err := sysFunction.WithContext(ctx).Where(sysFunction.Name.Eq("frontend_res")).First()
	if err != nil {
		return false
	}
	if info.EnableFlag == constant.DisableFlag {
		return false
	}
	return true
}

func UserIsShare(ctx context.Context) bool {
	var (
		sysFunction = query.SysFunction
	)
	// 默认没有该配置为关闭状态
	info, err := sysFunction.WithContext(ctx).Where(sysFunction.Name.Eq("shared_account")).First()
	if err != nil {
		return false
	}
	if info.EnableFlag == constant.DisableFlag {
		return false
	}
	return true
}

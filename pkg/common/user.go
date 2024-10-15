package common

import (
	constant "project/pkg/constant"
)

func CheckUserIsAdmin(authority string) bool {
	if authority == constant.SYS_ADMIN {
		return true
	}
	return false
}

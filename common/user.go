package common

import (
	constant "project/constant"
)

func CheckUserIsAdmin(authority string) bool {
	if authority == constant.SYS_ADMIN {
		return true
	}
	return false
}

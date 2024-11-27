package common

import (
	constant "project/pkg/constant"
)

func CheckUserIsAdmin(authority string) bool {
	return authority == constant.SYS_ADMIN
}

package utils

import (
	"golang.org/x/crypto/bcrypt"
)

// 加密密码 bcrypt.HashAndSalt([]byte("admin@protonmail.com"))
func HashAndSalt(pwd []byte) string {
	hash, err := bcrypt.GenerateFromPassword(pwd, bcrypt.MinCost)
	if err != nil {

	}
	return string(hash)
}

// 验证密码 bcrypt.ComparePasswords(o, []byte("admin@protonmail.com"))
func ComparePasswords(hashedPwd string, plainPwd []byte) bool {
	byteHash := []byte(hashedPwd)
	err := bcrypt.CompareHashAndPassword(byteHash, plainPwd)
	if err != nil {
		return false
	}
	return true
}

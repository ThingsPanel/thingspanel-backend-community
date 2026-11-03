package authcore

import "golang.org/x/crypto/bcrypt"

func bcryptMatches(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

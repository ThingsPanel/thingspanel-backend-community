package test

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"fmt"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// var password = "eDdka1FFVlR3enZ4ZVZLdVljQWt0RVNNT25mUHdpTU5tbXk0dkYzSThTcTUzRWN6VWl3STdIUzRTZTM1MXFROTl5V2xkbmtwOTQwZDVpZVl6b2NwZVF0RXNSc21aSmZ3a3RES3BwbVpWRURLNGJzZHVjSFhXTzd2eDY3VmFsQThjbjEwSnp2d0xNKzZVeHpiK2VnTTJqRUd6aFhTMGZEQ0ZmcEJPSEdmb1FMV1l5eTN3RWtZc2lFUzlxWjZ4WTlZbEN4Y2dibk9jeURuVFV0N3RlalM0UFMzR3BpMnFEWHRLWlFPVkpndEJqaTNWb1F2dG5yS3VpcURpSFhyaTdXVTRSY3BDbGcrb1UvLzcyc0FyN0huRkp1TjdWZHozSitmVFBWdWdiL0k2enhPQjhVVldsOUhxcit3UVkrZy9QckZZSWJ3RHVFSlBpVkpwbW5LUWROOUVRPT0="

var RSAPrivateKey *rsa.PrivateKey
var RSAPublicKey *rsa.PublicKey

func DecryptPassword(encryptedPassword string) ([]byte, error) {
	ciphertext, err := base64.StdEncoding.DecodeString(encryptedPassword)
	if err != nil {
		return nil, fmt.Errorf("解码密文失败: %v", err)
	}

	decrypted, err := rsa.DecryptPKCS1v15(rand.Reader, RSAPrivateKey, ciphertext)
	if err != nil {
		return nil, fmt.Errorf("解密失败: %v", err)
	}

	return decrypted, nil
}

func HashPassword(decryptedPassword []byte, _ []byte) (password []byte, err error) {
	hashedPassword, err := bcrypt.GenerateFromPassword(decryptedPassword, bcrypt.DefaultCost)
	if err != nil {
		return password, fmt.Errorf("密码哈希失败: %v", err)
	}
	return hashedPassword, err
}

func Encrypt() (string, error) {
	message := []byte("123456salt")
	encryptedMessage, err := rsa.EncryptPKCS1v15(rand.Reader, RSAPublicKey, message)
	if err != nil {
		return "", fmt.Errorf("加密失败: %w", err)
	}
	return base64.StdEncoding.EncodeToString(encryptedMessage), nil
}

func TestRSA(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	RSAPrivateKey = privateKey
	RSAPublicKey = &privateKey.PublicKey

	message := []byte("123456salt")
	password, err := Encrypt()
	if err != nil {
		t.Fatalf("encrypt message: %v", err)
	}
	passwords, err := DecryptPassword(password)
	if err != nil {
		t.Fatalf("decrypt ciphertext: %v", err)
	}
	if string(passwords) != string(message) {
		t.Fatalf("decrypted message = %q, want %q", passwords, message)
	}
}

package test

import (
	"project/initialize"
	"testing"
)

func TestRSA(t *testing.T) {
	initialize.RsaDecryptInit("../rsa_key/private_key.pem")
	t.Logf("%v", initialize.RSAPrivateKey)
}

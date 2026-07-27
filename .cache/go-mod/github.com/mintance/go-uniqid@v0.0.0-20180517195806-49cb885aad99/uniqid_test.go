package uniqid

import (
	"testing"
)

func TestWithoutPrefixAndEntropy(t *testing.T) {
	if id := New(Params{}); len(id) != 13 {
		t.Error("id length: ", len(id), " is not valid")
	}
}

func TestWithoutPrefixWithEntropy(t *testing.T) {
	if id := New(Params{MoreEntropy: true}); len(id) != 23 {
		t.Error("id with entropy length is not valid")
	}
}

func TestWithPrefixWithoutEntropy(t *testing.T) {
	if id := New(Params{Prefix: "test"}); len(id) != 17 || id[0:4] != "test" {
		t.Error("id with prefix length is invalid")
	}
}

func TestWithPrefixWithEntropy(t *testing.T) {
	if id := New(Params{Prefix: "test", MoreEntropy: true}); len(id) != 27 || id[0:4] != "test" {
		t.Error("id with prefix and entropy length is invalid")
	}
}

package tphttp

import (
	"fmt"
	"testing"
)

func TestTphttp_Post(t *testing.T) {
	response, err := Post("http://127.0.0.1:8083/v1/accounts/test", "{\"password\":\"test\"}")
	fmt.Println(response)
	fmt.Println(err)
}

func TestTphttp_Post_A(t *testing.T) {
	response, err := Post("http://127.0.0.1:8083/v1/accounts/test", "{\"password\":\"\"}")
	fmt.Println(response)
	fmt.Println(err)
}

func TestTphttp_Delete(t *testing.T) {
	response, err := Delete("http://127.0.0.1:8083/v1/accounts/test", "{}")
	fmt.Println(response)
	fmt.Println(err)

}

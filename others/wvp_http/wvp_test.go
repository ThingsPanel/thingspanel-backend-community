package wvp

import (
	"fmt"
	"testing"
)

func TestLogin(t *testing.T) {
	cookieValue, err := Login("http://119.91.238.241:18080", "admin", "admin")
	if err != nil {
		fmt.Println(err)
		t.Fail()
	}
	fmt.Println(cookieValue)
}
func TestGetDeviceChannels(t *testing.T) {
	bodyJson, err := GetDeviceChannels("http://119.91.238.241:18080", "44010200492000000001", "3DD86F5071587E87606CC9F4FD66FECA")
	if err != nil {
		fmt.Println(err)
		t.Fail()
	}
	fmt.Println(bodyJson)
}

func TestPtzControl(t *testing.T) {
	var queryMap = make(map[string]string)
	queryMap["command"] = "left"
	queryMap["horizonSpeed"] = "30"
	queryMap["verticalSpeed"] = "30"
	queryMap["zoomSpeed"] = "30"
	bodyJson, err := PtzControl("http://119.91.238.241:18080", "44010200492000000001", "34020000001320000001", "E034D431C67031694AEE9A71356064C6", queryMap)
	if err != nil {
		fmt.Println(err)
		t.Fail()
	}
	fmt.Println(bodyJson)
}
func TestGetPlayAddr(t *testing.T) {
	bodyJson, err := GetPlayAddr("http://119.91.238.241:18080", "44010200492000000001", "34020000001320000001", "5336336ADCE0A61009D6CC12E24CB938")
	if err != nil {
		fmt.Println(err)
		t.Fail()
	}
	fmt.Println(bodyJson)
}

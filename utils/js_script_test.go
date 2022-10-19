package utils

import (
	"fmt"
	"testing"
)

func TestScriptDeal(t *testing.T) {
	var code = `
	function encodeInp(msg, topic){
		if(topic == "1"){
			var obj = JSON.parse(msg)
			return obj.a
		}else{
			return msg
		}
	}
	`
	var msg = "{\"a\":\"abcc\"}"
	var topic = "1"
	response, err := ScriptDeal(code, msg, topic)
	if err != nil {
		t.Error(err.Error())
	} else {
		if response != "abcc" {
			t.Error("处理结果与预期不符！")
		}
	}
}

func TestScriptDeal1(t *testing.T) {
	var code = "function encodeInp(msg, topic){  var obj = JSON.parse(msg);obj.temp = 30;var m = JSON.stringify(obj);    return m;}"
	var msg = `{"temp":25.2}`
	var topic = "1"
	response, err := ScriptDeal(code, msg, topic)
	if err != nil {
		t.Error(err.Error())
	} else {
		fmt.Println(response)

	}
}

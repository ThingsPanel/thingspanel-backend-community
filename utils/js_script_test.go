package utils

import (
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

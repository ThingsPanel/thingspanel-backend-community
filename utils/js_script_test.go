package utils

import (
	"fmt"
	"testing"
)

func TestScriptDeal(t *testing.T) {
	var code = `
		if(topic == "1"){
			var obj = JSON.parse(msg)
			return obj.a
		}else{
			return msg
		}
	`
	var msg = "{\"a\":\"abc\"}"
	var topic = "1"
	response, err := ScriptDeal(code, msg, topic)
	fmt.Println(response)
	fmt.Println(err)
}

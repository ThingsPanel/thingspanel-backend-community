// 数据前置处理
package utils

import (
	"github.com/robertkrimen/otto"
)

func ScriptDeal(code string, msg string, topic string) (string, error) {
	/*
		function encodeInp(msg, topic){
			//编写脚本处理msg
			return msg
		}
	*/
	script := code
	vm := otto.New()
	_, err := vm.Run(script)
	if err != nil {
		return "", err
	}
	message, err := vm.Call("encodeInp", nil, msg, topic)
	if err != nil {
		return "", err
	}
	return message.String(), nil
}

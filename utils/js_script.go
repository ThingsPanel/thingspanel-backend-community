// 数据前置处理
package utils

import (
	"github.com/robertkrimen/otto"
)

func ScriptDeal(code string, msg string) (string, error) {
	script := `
	function encodeInp(msg){
		` + code + `
	}
	`
	vm := otto.New()
	_, err := vm.Run(script)
	if err != nil {
		return "", err
	}
	message, err := vm.Call("encodeInp", nil, msg)
	if err != nil {
		return "", err
	}
	return message.String(), nil
}

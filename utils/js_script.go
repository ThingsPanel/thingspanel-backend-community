// 数据前置处理
package utils

import (
	"github.com/beego/beego/v2/core/logs"
	"github.com/robertkrimen/otto"
)

func ScriptDeal(code string, msg interface{}, topic string) (string, error) {
	/*
		function encodeInp(msg, topic){
			//编写脚本处理从设备发来消息msg,转为平台可接收的消息规范
			//将msg转为json对象,如:var jsonObj = JSON.parse(msg);
			//处理完后将jsonObj转回字符串,如:msg = JSON.stringify(jsonObj);
			return msg;
		}
	*/
	/*
		function encodeInp(msg, topic){
			//编写脚本处理从平台发出的消息msg,转为设备可接收的消息规范
			//将msg转为json对象,如:var jsonObj = JSON.parse(msg);
			//处理完后将jsonObj转回字符串,如:msg = JSON.stringify(jsonObj);
			return msg;
		}
	*/
	logs.Info("执行脚本")
	script := code
	vm := otto.New()
	logs.Info(script)
	_, err := vm.Run(script)
	if err != nil {
		logs.Info(err.Error())
		return "", err
	}
	message, err := vm.Call("encodeInp", nil, msg, topic)
	if err != nil {
		logs.Info(err.Error())
		return "", err
	}
	logs.Info(message)
	return message.String(), nil
}

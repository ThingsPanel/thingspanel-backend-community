// 数据前置处理
package utils

import (
	"github.com/beego/beego/v2/core/logs"
	"github.com/robertkrimen/otto"
)

func ScriptDeal(code string, msg interface{}, topic string) (string, error) {
	/*
		function encodeInp(msg, topic){
			//将设备自定义msg（自定义形式）数据转换为json形式数据, 设备上报数据到物联网平台时调用
			//入参：topic string 设备上报消息的 topic
			//入参：msg byte[] 数组 不能为空
			//出参：string
			//处理完后将对象转回字符串形式
			//例，byte[]转string：var msgString = String.fromCharCode.apply(null, msg);
			//例，string转jsonObj：msgJson = JSON.parse(msgString);
			//例，jsonObj转string：msgString = JSON.stringify(msgJson);
			var msgString = String.fromCharCode.apply(null, msg);
			return msgString;
		}
	*/
	/*
		function encodeInp(msg, topic){
			//将平台规范的msg（json形式）数据转换为设备自定义形式数据, 物联网平台发送数据数到设备时调用
			//入参：topic string 设备订阅消息的 topic
			//入参：msg byte[] 数组 不能为空
			//出参：string
			//处理完后将对象转回字符串形式
			//例，byte[]转string：var msgString = String.fromCharCode.apply(null, msg);
			//例，string转jsonObj：msgJson = JSON.parse(msgString);
			//例，jsonObj转string：msgString = JSON.stringify(msgJson);
			var msgString = String.fromCharCode.apply(null, msg);
			return msgString;
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

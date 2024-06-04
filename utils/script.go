// data script somethings
package utils

import (
	luajson "github.com/layeh/gopher-json"
	"github.com/sirupsen/logrus"
	lua "github.com/yuin/gopher-lua"
)

func ScriptDeal(code string, msg []byte, topic string) (string, error) {
	/*
		function encodeInp(msg,topic)
		    // 该函数为编码函数，将输入的消息编码为平台可识别的消息格式或者设备可识别的消息格式
		  	// 入参： msg 为输入的消息(订阅或者上报的消息)，topic 为消息的主题 均为string类型
		  	// 出参： 返回值为string类型，为编码后的消息
		  	// 请根据实际需求编写编码逻辑
		   	// string与jsonObj互转需导入json库：local json = require("json")
		  	//例，string转jsonObj：local jsonTable = json.decode(msgString)
		  	//例，jsonObj转string：local json_str = json.encode(jsonTable)
		  	// 以下为示例代码
		  	// 只支持如下json包导入
		  	//处理完后将对象转回字符串形式
		  	local json = require("json")
		  	local jsonTable = json.decode(jsonString)
		  	if jsonTable.services[1]..service_id == "CO2" then
				jsonTable.services[1].properties.current = 200
		  	end
		  	local newJsonString = json.encode(jsonTable)
		  	return newJsonString
		 end
	*/

	L := lua.NewState()
	defer L.Close()

	L.PreloadModule("json", luajson.Loader)

	err := L.DoString(code)
	if err != nil {
		logrus.Error(err)
		return "", err
	}

	encodeInp := L.GetGlobal("encodeInp")
	err = L.CallByParam(lua.P{
		Fn:      encodeInp,
		NRet:    1,
		Protect: true,
	}, lua.LString(msg), lua.LString(topic))

	if err != nil {
		logrus.Error("Error executing Lua script:", err)
		return "", err
	}

	result := L.Get(-1)
	if result.Type() != lua.LTString {
		logrus.Error("Lua script must return a string")
		return "", err
	}
	return result.String(), nil
}

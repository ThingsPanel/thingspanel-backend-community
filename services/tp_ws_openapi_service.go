package services

import (
	"log"
	"net/http"
	"time"

	"github.com/beego/beego/v2/core/logs"
	"github.com/gorilla/websocket"
)

type TpWsOpenapi struct {
	//可搜索字段
	SearchField []string
	//可作为条件的字段
	WhereField []string
	//可做为时间范围查询的字段
	TimeField []string
}

func (*TpWsOpenapi) HandleConnections(w http.ResponseWriter, r *http.Request) {
	var upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}
	// 获取头信息示例
	key := r.Header.Get("Authorization")
	log.Printf("Received: %s", key)
	// 升级初始 GET 请求为 websocket
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logs.Error(err)
		return
	}

	// 关闭连接
	defer ws.Close()
	// 获取ip信息
	headers := ws.RemoteAddr().String()
	//[]byte转string
	ms := string(headers)
	log.Printf("Received: %s", ms)
	// 读取新的消息
	msgType, msg, err := ws.ReadMessage()
	if err != nil {
		logs.Error(err)
		return
	}
	log.Printf("Received: %s", msg)
	for {

		// 回复消息
		if err = ws.WriteMessage(msgType, msg); err != nil {
			logs.Error(err)
			return
		}
		// 等10秒
		time.Sleep(10 * time.Second)
	}
}

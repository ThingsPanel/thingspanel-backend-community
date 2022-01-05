package controllers

import (
	"ThingsPanel-Go/services"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	beego "github.com/beego/beego/v2/server/web"
	"github.com/gorilla/websocket"
)

type WebsocketController struct {
	beego.Controller
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

type Client struct {
	ID     string
	Conn   *websocket.Conn
	Ticker *time.Ticker
}

type Ch struct {
	JoinChan   chan *Client       //用户加入通道
	ExitChan   chan *Client       //用户退出通道
	MsgChan    chan string        //消息通道
	ClientList map[string]*Client //客户端用户列表
}

var AllCh = Ch{
	JoinChan:   make(chan *Client),
	ExitChan:   make(chan *Client),
	MsgChan:    make(chan string),
	ClientList: make(map[string]*Client),
}

type MsgContent struct {
	User    string `json:"user"` //用户
	Wid     string `json:"wid"`  //消息内容
	StartTs int64  `json:"startTs"`
	EndTs   int64  `json:"endTs"`
	Data    string `json:"data"`
}

// 解析token
func FormatQuery(url string, paramName string) string {
	urls := strings.Split(url, "?")
	strParam := urls[1]
	strArr := strings.Split(strParam, "&")
	OutMap := make(map[string]interface{})
	if strArr[0] != "" && len(strArr) > 0 {
		for _, str := range strArr {
			newArr := strings.Split(str, "=")
			key := newArr[0]
			value := newArr[1]
			OutMap[key] = value
		}
	}
	return fmt.Sprintf("%v", OutMap[paramName])
}

// 主程序
func (this *WebsocketController) WsHandler() {
	w := this.Ctx.ResponseWriter
	r := this.Ctx.Request
	go AllCh.Start()
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		fmt.Println(err)
		return
	}
	token := FormatQuery(fmt.Sprintf("%v", r.URL), "token")
	c := &Client{
		ID:     token,
		Conn:   conn,
		Ticker: nil,
	}
	AllCh.JoinChan <- c
	go c.ReadMsg()
}

func (ch *Ch) Start() {
	for {
		select {
		case v := <-ch.JoinChan:
			fmt.Println("用户加入", v.ID)
			AllCh.ClientList[v.ID] = v
		case v := <-ch.ExitChan:
			fmt.Println("用户退出", v.ID)
			if v.Ticker != nil {
				v.Ticker.Stop()
			}
			delete(AllCh.ClientList, v.ID)
		case v := <-ch.MsgChan:
			var msgContent MsgContent
			_ = json.Unmarshal([]byte(v), &msgContent)
			for id, conn := range AllCh.ClientList {
				if id == msgContent.User {
					conn.WriteMsg(v)
				}
			}
		}
	}
}

// 接收数据
func (c *Client) ReadMsg() {
	defer func() {
		AllCh.ExitChan <- c
		_ = c.Conn.Close()
	}()
	var WidgetService services.WidgetService
	var TSKVService services.TSKVService
	first := true
	var StartTs int64
	var EndTs int64
	c.Ticker = time.NewTicker(time.Millisecond * 10000)
	for {
		_, p, err := c.Conn.ReadMessage()
		if err != nil {
			break
		}
		go func() {
			if first {
				var msgContent MsgContent
				_ = json.Unmarshal(p, &msgContent)
				msgContent.User = c.ID
				w, _ := WidgetService.GetWidgetById(msgContent.Wid)
				device_ids := []string{w.DeviceID}
				et := time.Now().Unix()
				st := et - 1800
				StartTs = st * 1000
				EndTs = et * 1000
				fmt.Println(StartTs, ":", EndTs)
				data := TSKVService.GetTelemetry(device_ids, StartTs, EndTs)
				msg, _ := json.Marshal(data)
				msgContent.Data = string(msg)
				message, _ := json.Marshal(msgContent)
				AllCh.MsgChan <- string(message)
				first = false
			}
			for t := range c.Ticker.C {
				fmt.Println(t)
				var msgContent MsgContent
				_ = json.Unmarshal(p, &msgContent)
				msgContent.User = c.ID
				w, _ := WidgetService.GetWidgetById(msgContent.Wid)
				device_ids := []string{w.DeviceID}
				StartTs = EndTs
				EndTs = StartTs + 10000
				fmt.Println(StartTs, ":", EndTs)
				data := TSKVService.GetTelemetry(device_ids, StartTs, EndTs)
				msg, _ := json.Marshal(data)
				msgContent.Data = string(msg)
				message, _ := json.Marshal(msgContent)
				AllCh.MsgChan <- string(message)
			}
		}()
	}
}

// 发送数据
func (c *Client) WriteMsg(message string) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("Recovered in f=========>", r)
		}
	}()
	err := c.Conn.WriteMessage(websocket.TextMessage, []byte(message))
	if err != nil {
		fmt.Println(err)
	}
	fmt.Println("发送到客户端的信息:", message)
}

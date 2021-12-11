package controllers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

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
	ID   string
	Conn *websocket.Conn
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
	Sender   string `json:"sender"`   //发送者
	Receiver string `json:"receiver"` //接收者
	Content  string `json:"content"`  //消息内容
}

func (ch *Ch) Start() {
	for {
		select {
		case v := <-ch.JoinChan:
			fmt.Println("用户加入", v.ID)
			AllCh.ClientList[v.ID] = v
		case v := <-ch.ExitChan:
			fmt.Println("用户退出", v.ID)
			delete(AllCh.ClientList, v.ID)
		case v := <-ch.MsgChan:
			var msgContent MsgContent
			_ = json.Unmarshal([]byte(v), &msgContent)
			for id, conn := range AllCh.ClientList {
				if id == msgContent.Receiver {
					conn.WriteMsg(v)
				}
			}
		}
	}
}

func (c *Client) ReadMsg() {
	defer func() {
		AllCh.ExitChan <- c
		_ = c.Conn.Close()
	}()
	for {
		_, p, err := c.Conn.ReadMessage()
		if err != nil {
			break
		}
		var msgContent MsgContent
		_ = json.Unmarshal(p, &msgContent)
		msgContent.Sender = c.ID
		message, _ := json.Marshal(msgContent)
		fmt.Println("读取到客户端的信息:", string(message))
		AllCh.MsgChan <- string(message)
	}
}

func (c *Client) WriteMsg(message string) {
	err := c.Conn.WriteMessage(websocket.TextMessage, []byte(message))
	if err != nil {
		fmt.Println(err)
	}
	fmt.Println("发送到客户端的信息:", message)
}

func (this *WebsocketController) WsHandler() {
	w := this.Ctx.ResponseWriter
	r := this.Ctx.Request
	go AllCh.Start()

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		fmt.Println(err)
		return
	}
	uid := FormatQuery(fmt.Sprintf("%v", r.URL), "uid")
	c := &Client{
		ID:   uid,
		Conn: conn,
	}
	AllCh.JoinChan <- c
	go c.ReadMsg()
}

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

// func (this *WebsocketController) WsHandler() {
// 	fmt.Println("参数解析失败")
// 	response.SuccessWithMessage(400, "WebsocketController", (*context2.Context)(this.Ctx))
// 	return
// }

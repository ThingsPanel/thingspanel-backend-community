package controllers

import (
	"ThingsPanel-Go/services"
	"ThingsPanel-Go/utils"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/beego/beego/v2/core/logs"
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
	mutex  sync.Mutex
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
			fmt.Println("用户加入", utils.ReplaceUserInput(v.ID))
			AllCh.ClientList[v.ID] = v
		case v := <-ch.ExitChan:
			fmt.Println("用户退出", utils.ReplaceUserInput(v.ID))
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
	var StartTs int64
	var EndTs int64
	c.Ticker = time.NewTicker(time.Millisecond * 1000)
	for {
		_, p, err := c.Conn.ReadMessage()
		if err != nil {
			break
		}
		first := true
		go func() {
			// time_quantum的时间单位是分;默认是三天的时间区间
			var time_quantum int64 = 0
			// refresh_rate的时间单位是秒;默认是10秒
			var refresh_rate int64 = 0
			// sampling_rate的时间单位是秒;默认是10秒
			var sampling_rate string = ""
			if first {
				var msgContent MsgContent
				_ = json.Unmarshal(p, &msgContent)
				msgContent.User = c.ID
				w, _ := WidgetService.GetWidgetById(msgContent.Wid)

				if w.Extend != "" {
					extendMap := make(map[string]string)
					err := json.Unmarshal([]byte(w.Extend), &extendMap)
					if err == nil {
						if extendMap["time_quantum"] != "" {
							time, err := strconv.ParseInt(extendMap["time_quantum"], 10, 64)
							if err == nil {
								logs.Info(utils.ReplaceUserInput(msgContent.Wid), "图表的时间区间是：", time_quantum, "分")
								time_quantum = time
							}

						}
						if extendMap["refresh_rate"] != "" {
							rate, err := strconv.ParseInt(extendMap["refresh_rate"], 10, 64)
							if err == nil {
								logs.Info(utils.ReplaceUserInput(msgContent.Wid), "图表的刷新率是：", rate, "秒")
								refresh_rate = rate
							}

						}
						if extendMap["sampling_rate"] != "" {
							rate, err := strconv.Atoi(extendMap["sampling_rate"])
							if err == nil {
								logs.Info(utils.ReplaceUserInput(msgContent.Wid), "图表的采样率是：", rate, "秒")
								sampling_rate = strconv.Itoa(rate * 1000000)
							}

						}
					}
				}
				device_ids := []string{w.DeviceID}
				et := time.Now().Unix()
				if time_quantum == int64(0) {
					st := et - 3*24*60*60
					StartTs = st * 1000
				} else {
					StartTs = (et - time_quantum*60) * 1000
				}
				EndTs = et * 1000
				fmt.Println(StartTs, ":", EndTs)

				data := TSKVService.GetTelemetry(device_ids, StartTs, EndTs, sampling_rate)
				msg, _ := json.Marshal(data)
				msgContent.Data = string(msg)
				message, _ := json.Marshal(msgContent)
				AllCh.MsgChan <- string(message)
				first = false
			}
			for t := range c.Ticker.C {
				thisTime := time.Now().Unix()
				var rate_time int64 = 0
				if refresh_rate == int64(0) {
					rate_time = 10000
				} else {
					rate_time = refresh_rate * 1000
				}
				// 十秒一次推送
				if thisTime*1000-EndTs > rate_time {
					fmt.Println(t)
					var msgContent MsgContent
					_ = json.Unmarshal(p, &msgContent)
					msgContent.User = c.ID
					w, _ := WidgetService.GetWidgetById(msgContent.Wid)
					device_ids := []string{w.DeviceID}
					StartTs = EndTs
					EndTs = StartTs + rate_time
					fmt.Println(StartTs, ":", EndTs)
					data := TSKVService.GetTelemetry(device_ids, StartTs, EndTs, sampling_rate)
					var dataMap = data[0].(map[string]interface{})
					//if dataMap["fields"] is null,do not send for ws
					if fmt.Sprintf("%T", dataMap["fields"]) != "[]string" {
						msg, _ := json.Marshal(data)
						msgContent.Data = string(msg)
						message, _ := json.Marshal(msgContent)
						AllCh.MsgChan <- string(message)
					}
				}
			}
		}()
	}
}

// 发送数据
func (c *Client) WriteMsg(message string) {
	//defer func() {
	//	if r := recover(); r != nil {
	//		fmt.Println("Recovered in f=========>", r)
	//	}
	//}()
	c.mutex.Lock()
	err := c.Conn.WriteMessage(websocket.TextMessage, []byte(message))
	c.mutex.Unlock()
	if err != nil {
		fmt.Println(err)
	}
	fmt.Println("发送到客户端的信息:", len(message))
}

package tcp

import (
	cache "ThingsPanel-Go/initialize/cache"
	"ThingsPanel-Go/services"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/fwhezfwhez/tcpx"
)

// var requestTimes int32
var TSKVS services.TSKVService

func Listen(tcpPort string) {
	srv := tcpx.NewTcpX(nil)
	srv.UseGlobal(func(c *tcpx.Context) {
		fmt.Println("tcp link!")
	})
	srv.HandleRaw = func(c *tcpx.Context) {
		var buf []byte
		var upBuf []byte
		var readBuf = make([]byte, 20000)
		var n int
		var e error
		for {
			n = 0
			buf = []byte{}
			fmt.Println("upBuf len:", len(upBuf))
			if len(upBuf) < 4 {
				n, e = c.ConnReader.Read(readBuf)
				if e != nil {
					fmt.Println(e.Error())
					return
				}
				buf = append(buf, readBuf[:n]...)
				fmt.Println("buf len:", len(buf))
			}
			buf = append(upBuf, buf...)
			n = n + len(upBuf)
			upBuf = []byte{}
			//fmt.Println("receiveData:", buf[:n])
			fmt.Println("receiveData:", n)
			// 判断帧头，不正确就跳过
			if !bytes.EqualFold(buf[0:2], []byte{0xff, 0xfe}) {
				fmt.Println("frameHeader error!")
				c.ConnWriter.Write([]byte("frameHeader error!"))
				continue
			}
			pkgSizeTotal := int(buf[2])*256 + int(buf[3])
			fmt.Println("pkgSizeTotal:", pkgSizeTotal)
			if n < pkgSizeTotal {
				for {
					var tempBuf = make([]byte, 20000)
					nn, e := c.ConnReader.Read(tempBuf)
					if e != nil {
						fmt.Println(e.Error())
						return
					}
					if n+nn > pkgSizeTotal {
						buf = append(buf, tempBuf[:pkgSizeTotal-n]...)
						upBuf = tempBuf[pkgSizeTotal-n : nn]
						break
					} else if n+nn < pkgSizeTotal {
						buf = append(buf, tempBuf[:nn]...)
						n = n + nn
					} else {
						buf = append(buf, tempBuf[:nn]...)
						n = n + nn
						break
					}
				}
			} else if n > pkgSizeTotal {
				upBuf = buf[pkgSizeTotal:]
				buf = buf[:pkgSizeTotal]
				n = len(buf)
			}
			msgLength := int(buf[5])*256 + int(buf[6]) //消息长度
			fmt.Printf("msgLength:(%d)\n", msgLength)
			msgData := buf[7 : 7+msgLength] //消息
			fmt.Println("msgData:", msgData)
			fmt.Println("msgData:", string(msgData))
			var jsonMsg map[string]interface{} //消息解析到map
			err := json.Unmarshal(msgData, &jsonMsg)
			if err != nil {
				fmt.Println(err)
			}
			if _, ok := jsonMsg["token"]; ok {
				log.Println("token:", jsonMsg["token"])
				// 让设备重置命令
				s, _ := cache.Bm.IsExist(context.TODO(), jsonMsg["token"].(string))
				if s {
					cacheToken, _ := cache.Bm.Get(context.TODO(), jsonMsg["token"].(string))
					if cacheToken != 0 {
						if value, ok := cacheToken.([]byte); ok {
							resetMsg := buf[:7]
							resetMsg[4] = 0x02
							resetMsg[5] = 0xee
							resetMsg[6] = uint8(len(value) % 256)
							resetMsg = append(resetMsg, value...)
							resetMsg = append(resetMsg, byte(0xfd))
							resetMsg[2] = 0xee
							resetMsg[3] = uint8(len(resetMsg) % 256)
							c.ConnWriter.Write(resetMsg)
						}
					}
				}
				cache.Bm.Put(context.TODO(), jsonMsg["token"].(string), 0, 600*time.Second)
			} else {
				c.ConnWriter.Write([]byte("token error!"))
				continue
			}
			//消息ID（0x10-心跳包，0x20-数据包）
			if buf[4] == byte(0x10) || buf[4] == byte(0x09) {
				//#心跳包应答#---
				TSKVS.MsgProc(msgData)
				buf[4] = 0x11
				c.ConnWriter.Write(buf[:n])
			} else if buf[4] == byte(0x01) {
				// 控制反馈包
				continue
			} else if buf[4] == byte(0x20) {
				//1-解析包；2-落地文件记录 3-最后一包写完后更新文件状态记录kv
				//#数据包请求#---                                        //最大包号
				currentNo := int(buf[7+msgLength])*256 + int(buf[8+msgLength]) //当前包号
				//fmt.Printf("currentNo:%d\n", currentNo)
				maxNo := int(buf[9+msgLength])*256 + int(buf[10+msgLength]) //最大包号
				//fmt.Printf("maxNo:%d\n", maxNo)
				imgDataLength := int(buf[11+msgLength])*256 + int(buf[12+msgLength])
				fmt.Printf("imgdataLength:%d\n", imgDataLength)
				imgData := buf[13+msgLength : 13+msgLength+imgDataLength] //数据
				var valuesMap map[string]interface{}
				if _, ok := jsonMsg["values"]; ok {
					valuesMap = jsonMsg["values"].(map[string]interface{})
					if err != nil {
						fmt.Println(err)
					}
					fmt.Println("filename:", valuesMap["filename"].(string))
				} else {
					c.ConnWriter.Write([]byte("filename error!"))
					continue
				}
				writeFile(imgData, valuesMap["filename"].(string)) //写文件
				if currentNo == 0 {
					if maxNo == 0 {
						timeStr := time.Now().Format("2006-01-02")
						var newFilename interface{} = "/files/img/" + timeStr + "/" + valuesMap["filename"].(string)
						valuesMap["filename"] = newFilename
						jsonMsg["values"] = valuesMap
						newJson, err := json.Marshal(jsonMsg)
						if err != nil {
							fmt.Println("json.Marshal failed:", err)
							return
						}
						fmt.Println(string(newJson))
						//直接记录kv
						TSKVS.MsgProc(newJson)
					}
				} else if maxNo == currentNo {
					timeStr := time.Now().Format("2006-01-02")
					var newFilename interface{} = "/files/img/" + timeStr + "/" + valuesMap["filename"].(string)
					valuesMap["filename"] = newFilename
					jsonMsg["values"] = valuesMap
					newJson, err := json.Marshal(jsonMsg)
					if err != nil {
						fmt.Println("json.Marshal failed:", err)
						return
					}
					fmt.Println(string(newJson))
					//直接记录kv
					TSKVS.MsgProc(newJson)
				}
				buf[4] = 0x21
				c.ConnWriter.Write(append(buf[:7+msgLength], byte(0xfd))) //回复客户端
			} else {
				fmt.Println("000201:Package type error!")
				c.ConnWriter.Write([]byte("000201:Package type error!"))
			}
		}
	}
	fmt.Println(tcpPort)
	go func() {
		srv.ListenAndServeRaw("tcp", tcpPort)
		time.Sleep(1 * time.Second)
		if e := srv.Stop(false); e != nil {
			fmt.Println(e.Error())
		}
	}()
}
func writeFile(data []byte, filename string) {
	timeStr := time.Now().Format("2006-01-02")
	os.MkdirAll("./files/img/"+timeStr, os.ModePerm)
	filePath := "./files/img/" + timeStr + "/" + filename
	file, err := os.OpenFile(filePath, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0666)
	if err != nil {
		fmt.Println("文件打开失败", err)
	}
	defer file.Close()
	//写入文件时，使用带缓存的 *Writer
	write := bufio.NewWriter(file)
	write.Write(data)
	//Flush将缓存的文件真正写入到文件中
	write.Flush()
}

// func countRequestTime(c *tcpx.Context) {
// 	atomic.AddInt32(&requestTimes, 1)
// }

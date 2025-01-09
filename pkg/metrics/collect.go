// pkg/metrics/metrics.go
package metrics

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"
)

type InstanceInfo struct {
	InstanceID string
	Address    string
	Count      int64
	Timestamp  string
}

func NewInstance() *InstanceInfo {
	return &InstanceInfo{}
}

func (ins *InstanceInfo) Instan() {

	ins.Timestamp = strconv.FormatInt(time.Now().Unix(), 10)

	addrs, err := net.InterfaceAddrs()
	if err != nil {
		ins.Address = ""
		return
	}

	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				ins.Address = ipnet.IP.String()
				break
			}
		}
	}

	interfaces, err := net.Interfaces()
	timestamp := time.Now().UnixNano()

	if err != nil {
		ins.InstanceID = fmt.Sprintf("%s-%d", "null", timestamp)
		return
	}

	for _, i := range interfaces {
		if i.Flags&net.FlagLoopback == 0 && i.HardwareAddr != nil {
			ins.InstanceID = fmt.Sprintf("%s-%d", i.HardwareAddr.String(), timestamp)
			return
		}
	}
	ins.InstanceID = fmt.Sprintf("%s-%d", "null", timestamp)
}

func generateHMAC(message, secret string) string {
	key := []byte(secret)
	h := hmac.New(sha256.New, key)
	h.Write([]byte(message))
	signature := h.Sum(nil)
	return hex.EncodeToString(signature)
}

// SendSignedRequest 发送带签名的请求
func (ins *InstanceInfo) SendSignedRequest() {

	signature := generateHMAC(ins.Timestamp, "1hj5b0sp9")

	jsonMessage, _ := json.Marshal(ins)
	req, err := http.NewRequest("POST", "http://stats.thingspanel.cn/api/v1/c", bytes.NewBuffer(jsonMessage))
	if err != nil {
		return
	}
	req.Header.Set("X-Signature", "sha256="+signature)
	req.Header.Set("X-Timestamp", ins.Timestamp)
	req.Header.Set("Content-Type", "application/json")

	// Sending the request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
}

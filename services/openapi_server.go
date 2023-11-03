package services

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"

	"github.com/beego/beego/logs"
	"github.com/spf13/viper"
)

func init() {
	go func() {

		mux := http.NewServeMux()
		mux.HandleFunc("/", handler)
		port := viper.GetString("openapi.httpport")
		err := http.ListenAndServe(":"+port, mux)
		if err != nil {
			logs.Error("OpenApi服务启动失败", err.Error())
		}
	}()

}

func handler(w http.ResponseWriter, req *http.Request) {

	body, err := ioutil.ReadAll(req.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	req.Body = ioutil.NopCloser(bytes.NewReader(body))
	apiPort := viper.GetString("openapi.httpport")
	url := fmt.Sprintf("%s://%s%s", "http", "127.0.0.1:"+apiPort, req.RequestURI)

	proxyReq, err := http.NewRequest(req.Method, url, bytes.NewReader(body))
	//头信息拷贝
	proxyReq.Header = make(http.Header)
	for h, val := range req.Header {
		proxyReq.Header[h] = val
	}

	httpClient := &http.Client{}
	resp, err := httpClient.Do(proxyReq)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	//拷贝返回体
	for name, values := range resp.Header {
		w.Header()[name] = values
	}
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

package wvp

import (
	"crypto/md5"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/beego/beego/v2/core/logs"
	"github.com/bitly/go-simplejson"
)

func MD5(str string) string {
	data := []byte(str) //切片
	has := md5.Sum(data)
	md5str := fmt.Sprintf("%x", has) //将[]byte转成16进制
	return md5str
}
func Login(host string, username string, password string) (string, error) {

	url := host + "/api/user/login?password=" + MD5(password) + "&username=" + username
	res, err := WvpHttpGetReq(url, "", nil)
	if err != nil {
		return "", err
	}
	cookies := res.Cookies()
	logs.Info(res.Cookies())
	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return "", err
	}
	bodyJson, _ := simplejson.NewJson(body)
	if bodyJson.Get("code").MustString() == "-1" {
		return "", errors.New(bodyJson.Get("msg").MustString())
	}
	return cookies[0].Value, nil
}

// 查询设备的通道
func GetDeviceChannels(host string, deviceId string, cookieValue string) (*simplejson.Json, error) {
	url := host + "/api/device/query/devices/" + deviceId + "/channels"
	var queryMap = make(map[string]string)
	queryMap["count"] = "100"
	queryMap["page"] = "1"
	queryMap["online"] = "true"
	res, err := WvpHttpGetReq(url, cookieValue, queryMap)
	if err != nil {
		return nil, err
	}
	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	bodyJson, _ := simplejson.NewJson(body)
	return bodyJson, nil
}

// 云台控制
func PtzControl(host string, deviceId string, channelId string, cookieValue string, queryMap map[string]string) (string, error) {
	url := host + "/api/ptz/control/" + deviceId + "/" + channelId
	res, err := WvpHttpPostReq(url, cookieValue, queryMap)
	if err != nil {
		return "", err
	}
	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// 获取监控播放地址
func GetPlayAddr(host string, deviceId string, channelId string, cookieValue string) (*simplejson.Json, error) {
	url := host + "/api/play/start/" + deviceId + "/" + channelId
	res, err := WvpHttpGetReq(url, cookieValue, nil)
	if err != nil {
		return nil, err
	}
	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	bodyJson, _ := simplejson.NewJson(body)
	simplejson.New()
	return bodyJson, nil
}

// 获取通道录像列表
func GetVideoList(host string, deviceId string, channelId string, cookieValue string, queryMap map[string]string) (*simplejson.Json, error) {
	url := host + "/api/gb_record/query/" + deviceId + "/" + channelId
	res, err := WvpHttpGetReq(url, cookieValue, queryMap)
	if err != nil {
		return nil, err
	}
	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	bodyJson, _ := simplejson.NewJson(body)
	simplejson.New()
	return bodyJson, nil
}

// 获取录像播放地址
func GetPlaybackAddr(host string, deviceId string, channelId string, cookieValue string, queryMap map[string]string) (*simplejson.Json, error) {
	url := host + "/api/playback/start/" + deviceId + "/" + channelId
	res, err := WvpHttpGetReq(url, cookieValue, queryMap)
	if err != nil {
		return nil, err
	}
	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	bodyJson, _ := simplejson.NewJson(body)
	simplejson.New()
	return bodyJson, nil
}

//wvp get请求
func WvpHttpGetReq(url string, cookieValue string, queryMap map[string]string) (*http.Response, error) {
	client := &http.Client{}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	var cookie http.Cookie
	cookie.Name = "JSESSIONID"
	cookie.Value = cookieValue
	req.AddCookie(&cookie)
	q := req.URL.Query()
	for key, value := range queryMap {
		q.Add(key, value)
	}
	req.URL.RawQuery = q.Encode()
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	return res, nil
}

//wvp post请求
func WvpHttpPostReq(url string, cookieValue string, queryMap map[string]string) (*http.Response, error) {
	client := &http.Client{}
	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		return nil, err
	}

	var cookie http.Cookie
	cookie.Name = "JSESSIONID"
	cookie.Value = cookieValue
	req.AddCookie(&cookie)
	q := req.URL.Query()
	for key, value := range queryMap {
		q.Add(key, value)
	}
	req.URL.RawQuery = q.Encode()
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	return res, nil
}

package tphttp

import (
	"bytes"
	"errors"
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/beego/beego/v2/core/logs"
)

func Post(targetUrl string, payload string) (*http.Response, error) {
	req, _ := http.NewRequest("POST", targetUrl, strings.NewReader(payload))
	req.Header.Add("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(req)
	if err == nil {
		logs.Info(response.Body)
	} else {
		logs.Info(err.Error())
	}
	return response, err
}

func Delete(targetUrl string, payload string) (*http.Response, error) {
	logs.Info("Delete:", targetUrl, payload)
	req, _ := http.NewRequest("DELETE", targetUrl, strings.NewReader(payload))
	req.Header.Add("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(req)
	if err == nil {
		logs.Info(response.Body)
	} else {
		logs.Info(err.Error())
	}
	return response, err
}

func Get(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		logs.Error(err.Error())
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 200 {
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
		logs.Info("Response: ", string(body))
		return body, err
	} else {
		return nil, errors.New("Get failed with error: " + resp.Status)
	}
}
func PostJson(targetUrl string, payload []byte) (*http.Response, error) {
	req, _ := http.NewRequest("POST", targetUrl, bytes.NewReader(payload))
	req.Header.Add("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(req)
	return response, err
}

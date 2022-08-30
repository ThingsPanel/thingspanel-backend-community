package tphttp

import (
	"net/http"
	"strings"
)

func Post(targetUrl string, payload string) (*http.Response, error) {
	req, _ := http.NewRequest("POST", targetUrl, strings.NewReader(payload))
	req.Header.Add("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(req)
	return response, err
}

func Delete(targetUrl string, payload string) (*http.Response, error) {
	req, _ := http.NewRequest("DELETE", targetUrl, strings.NewReader(payload))
	req.Header.Add("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(req)
	return response, err
}

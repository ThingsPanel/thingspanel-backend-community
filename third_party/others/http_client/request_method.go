package http_client

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/sirupsen/logrus"
)

// Post 发送post请求
func Post(targetUrl string, payload string) (*http.Response, error) {
	req, _ := http.NewRequest("POST", targetUrl, strings.NewReader(payload))
	req.Header.Add("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		logrus.Info(err.Error())
	}
	return response, err
}

// Delete 发送delete请求
func Delete(targetUrl string, payload string) (*http.Response, error) {
	logrus.Info("Delete:", targetUrl, payload)
	req, _ := http.NewRequest("DELETE", targetUrl, strings.NewReader(payload))
	req.Header.Add("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(req)
	if err == nil {
		logrus.Info(response.Body)
	} else {
		logrus.Info(err.Error())
	}
	return response, err
}

// Get 发送get请求
func Get(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		logrus.Error(err.Error())
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 200 {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
		logrus.Info("Response: ", string(body))
		return body, err
	} else {
		return nil, errors.New("Get failed with error: " + resp.Status)
	}
}

// PostWithHeader 发送带有header的post请求
func PostJson(targetUrl string, payload []byte) (*http.Response, error) {
	req, err := http.NewRequest("POST", targetUrl, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Add("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(req)
	return response, err
}

func generateHMAC(message, secret string) string {
	key := []byte(secret)
	h := hmac.New(sha256.New, key)
	h.Write([]byte(message))
	signature := h.Sum(nil)
	return hex.EncodeToString(signature)
}

// SendSignedRequest 发送带签名的请求
func SendSignedRequest(url, message, secret string) error {
	signature := generateHMAC(message, secret)

	// Creating the request
	req, err := http.NewRequest("POST", url, bytes.NewBufferString(message))
	if err != nil {
		return fmt.Errorf("创建请求失败: %v", err)
	}

	// Adding the signature to the request header
	req.Header.Set("X-Signature-256", "sha256="+signature)
	req.Header.Set("Content-Type", "application/json")

	// Sending the request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("发送请求失败: %v", err)
	}
	defer resp.Body.Close()

	fmt.Printf("请求已发送。状态码: %d\n", resp.StatusCode)
	return nil
}

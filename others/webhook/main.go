package main

import (
	"ThingsPanel-Go/utils"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os/exec"
)

func main() {
	http.HandleFunc("/payload", payload)
	fmt.Println("启动服务...4567")
	http.ListenAndServe("0.0.0.0:4567", nil)
}
func payload(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		result, _ := ioutil.ReadAll(r.Body)
		fmt.Printf("%s\n", utils.ReplaceUserInput(string(result)))
		_, _ = io.WriteString(w, "Success")
		go shell()
	}
}
func shell() {
	fmt.Println("开始执行脚本...")
	//需要执行的命令： free -mh
	cmd := exec.Command("/bin/bash", "-c", `./z-shell.sh`)
	// 获取管道输入
	output, err := cmd.StdoutPipe()
	if err != nil {
		fmt.Println("无法获取命令的标准输出管道", err.Error())
		return
	}
	// 执行Linux命令
	if err := cmd.Start(); err != nil {
		fmt.Println("Linux命令执行失败，请检查命令输入是否有误", err.Error())
		return
	}
	// 读取所有输出
	bytes, err := ioutil.ReadAll(output)
	if err != nil {
		fmt.Println("打印异常，请检查")
		return
	}
	if err := cmd.Wait(); err != nil {
		fmt.Println("Wait", err.Error())
		return
	}
	fmt.Printf("打印信息：\n\n%s", bytes)
}

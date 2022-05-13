
# ThingsPanel-Go
## 环境版本
Golang(Go 1.17.5)（下载地址：https://golang.google.cn/dl/）

    $ wget https://golang.google.cn/dl/go1.17.6.linux-amd64.tar.gz
    $ tar -C /usr/local -zxvf go1.17.6.linux-amd64.tar.gz
    $ vim /etc/profile
    将 export PATH=$PATH:/usr/local/go/bin 添加到文件底部
    $ source /etc/profile（让配置生效）
    $ go version(查看版本)
## 后端配置文件
    conf/app.conf（系统配置文件）
    modules/dataService/config.yml（mqtt客户端、tcp端口配置）
## 插件目录
    extensions/
## 日志存放目录
    files/logo/
## 编译和运行
main.go文件的目录下对go代码进行编译和运行

    $ go build
    $ go run ThingsPanel-Go
## 数据库脚本
TP.sql
## 数据库默认用户
默认账户和密码
admin@thingspanel.cn 123456

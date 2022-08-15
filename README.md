
# ThingsPanel-Go

## 环境版本及linux安装示例

Golang-v1.17.6（下载地址：<https://golang.google.cn/dl/>）

```bash
wget https://golang.google.cn/dl/go1.17.6.linux-arm64.tar.gz
tar -C /usr/local -zxvf go1.17.6.linux-amd64.tar.gz
vim /etc/profile #将"export PATH=$PATH:/usr/local/go/bin"添加到文件底部
source /etc/profile #（让配置生效）
go version #(查看版本)
```

## 后端相关配置文件

```text
./conf/app.conf                  --系统配置 
./modules/dataService/config.yml --mqtt客户端、tcp端口配置
./gateway/bl/bl_config.yml       --网关转换接入案例的配置
```

## 插件目录

```text
./extensions/
```

## 日志存放目录

```text
./files/logs/
```

## 编译和运行

main.go文件的目录下对go代码进行编译和运行

```bash
go build #编译
./ThingsPanel-Go #或者以守护方式运行
```

## 数据库脚本

```text
./TP.sql
```

## 数据库默认超级用户

```text
账户:super@super.cn
密码:123456
```

## 产品文档

<http://thingspanel.io/>

## 联系我们

```text
qq群：260150504
```

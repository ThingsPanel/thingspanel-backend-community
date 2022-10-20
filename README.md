
# ThingsPanel-Go

<p>
<a href="https://golang.org/"><img alt="Go" src="https://img.shields.io/badge/-Go-00ACD7?logo=go&logoColor=white&style="/></a>
<a href="https://redis.io/"><img alt="redis" src="https://img.shields.io/badge/-redis-red?logo=redis&logoColor=white&style="/></a>
<a href="https://www.postgresql.org/"><img alt="redis" src="https://img.shields.io/badge/-postgresql-blue?logo=postgresql&logoColor=white&style="/></a>
<a href="https://www.docker.com/"><img alt="Docker" src="https://img.shields.io/badge/-Docker-2496ED?logo=Docker&logoColor=white&style="/></a>
</p>

目录：

- [ThingsPanel-Go](#thingspanel-go)
  - [环境版本及linux安装示例](#环境版本及linux安装示例)
  - [后端相关配置文件](#后端相关配置文件)
  - [日志存放目录](#日志存放目录)
  - [编译和运行](#编译和运行)
  - [数据库脚本](#数据库脚本)
  - [接口文档](#接口文档)
  - [产品文档](#产品文档)
  - [Demo地址](#demo地址)
  - [参与讨论和贡献](#参与讨论和贡献)


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
./modules/dataService/config.yml --mqtt客户端等
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

## 接口文档

<https://www.apifox.cn/apidoc/shared-34b48097-8c3a-4ffe-907e-12ff3c669936>

## 产品文档

<http://thingspanel.io/>

## Demo地址

<http://dev.thingspanel.cn/>

```text
账户:admin@thingspanel.cn
密码:123456
```

## 参与讨论和贡献

qq群：260150504  
欢迎有兴趣者加入沟通和讨论  
参与贡献请联系群主

#!/bin/bash
cd ../..
echo "git pull"
git pull
echo "开始编译部署..."
go build || (echo "编译失败退出..."; exit)
echo "编译完成"
supervisorctl stop beego
supervisorctl start beego

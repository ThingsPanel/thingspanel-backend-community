# Gen

## 参考资料

https://gorm.io/zh_CN/gen/database_to_structs.html

## 说明

1. 创建表结构后，使用Gen生成model和query
2. 修改./main中**第29行**g.GenerateModel("users")，将users改为自己要生成的表名
3. 在当前目录下运行：go run .
4. 操作会覆盖/query/gen.go文件，将内容复制下来，还原gen.go文件后，将复制下来的内容重新补充进去
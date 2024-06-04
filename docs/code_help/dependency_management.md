# 依赖管理相关

## 常用命令

1. go mod tidy -compat=1.18
   指定在运行 go mod tidy 时应该考虑的 Go 版本兼容性。

2. go list -m -versions github.com/xxx
   查看包的版本列表

3. 在终端中使用go get xxx来下载包，不要在代码中点击下载（会导致下载最新的包可能和版本不兼容）
   
  
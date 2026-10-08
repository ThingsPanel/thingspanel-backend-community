# 单元测试

## 单元测试文件
- 单元测试文件命名规则为`xxx_test.py`
- 一般单元测试文件存放路径`./test`
- 也可以放在模块的同级目录下，方便对代码就近测试，例如：`./api/test/board_api_test.py`


## 测试函数
- 测试函数命名规则`TestXXX`
- 测试函数第一个参数必须是 `t *testing.T`
- 测试函数第一行必须要初始化 require 对象`require := require.New(t)   // initialize the requireion library`

示例：
```
def test_add(t *testing.T):
    require.Equal(123, 123, "they should be equal")
```

## 运行测试
- 全量 Go 测试：`go test ./...`。
- 数据库集成测试必须设置 `TEST_DATABASE_URL`，不再通过 `run_env` 选择环境，也不会跳过。
- 连接串必须指向本机或回环地址，且数据库名以 `thingspanel_test_` 开头。用例会清空该库的整个 `public` schema，并要求数据库已安装 TimescaleDB 扩展；禁止指向共享库或线上库。
- 示例：`TEST_DATABASE_URL='postgres://<用户>:<密码>@127.0.0.1:5432/thingspanel_test_local' go test ./...`。

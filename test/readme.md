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
- 默认运行：`go test ./...`。未设置数据库测试环境时，数据库集成用例会明确跳过；其他 Go 测试仍正常执行。
- `pg_test.go` 会执行 `DROP SCHEMA public CASCADE`，会清空配置数据库的整个 `public` schema。只有配置确认指向专用、可销毁的隔离测试数据库时，才可设置 `run_env=localdev` 或 `run_env=git-actions` 运行数据库集成用例；禁止指向共享库或线上库。

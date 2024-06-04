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
- 运行测试命令：`run_env=localdev go test -v ./...`
- 运行测试命令会自动查找`./test`目录下所有以`*_test.py`结尾的文件，并执行测试函数。
- 如果测试函数中有`t.Error()`或`t.Fail()`，则测试失败。
- 如果测试函数中没有`t.Error()`或`t.Fail()`，则测试成功。

- 在本地环境中一键测试项目中所有的单元测试：`run_env=localdev go test -v ./...`
- 请提前改好configs/conf-localdev.yaml中的数据库配置，并确保本地环境中已经启动了数据库。
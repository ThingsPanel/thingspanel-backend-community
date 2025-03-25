# 项目说明

## APIfox文档（暂不维护swagger文档，转到apifox）

>注意：不要使用apifox的导入功能，操作不当会覆盖掉所有已编写的接口

`APIfox文档规范` https://docs.qq.com/doc/DZVZKc2FCTE1EblBX

## Gen（必读）

**model和query生成**：[查看README文档](/cmd/gen/REANME.md)

## 运行调试

**运行**：go run .
**接口调试**：http://localhost:9999/swagger/index.html

## 注意事项

- 日志使用logrus
- 获取配置文件使用viper，取不到值要设置默认值
- model和query都用gen工具生成
- 初次编码，请参考用户管理、通知组相关接口示例
- 编码时候注意权限的逻辑
  - 用户共分三种，通过用户的authority字段区分（TENANT_ADMIN-租户管理员 TENANT_USER-租户用户 SYS_ADMIN-系统管理员）
  - 每个租户都有一个租户管理员，租户之前的数据是隔离的，比如用户是租户管理员或者租户用户，请求过来后后端只能去查这个租户的数据，表中以tenant_id区分租户
  - 系统管理员账号：super@super.cn 123456
  - 系统管理员登录后，可在租户管理中创建租户管理员；以租户管理员登录，可在用户管理可以创建租户用户
- 新增的sql要更新的/sql/1.sql文件中
- 指针赋值转换service/enter.go里有公共方法
- 由于数据库在写入时候的报错不友好，所以需要手动校验json字段，如

  ```go
    // 校验是否json格式字符串
    if CreateDeviceConfigReq.AdditionalInfo != nil && !IsJSON(*CreateDeviceConfigReq.AdditionalInfo) {
      return fmt.Errorf("additional_info is not a valid JSON")
    }
    deviceconfig.AdditionalInfo = CreateDeviceConfigReq.AdditionalInfo
  ```

  或者在转map过程中校验

  ```go
    condsMap, err := StructToMapAndVerifyJson(req, "additional_info", "protocol_config")
    if err != nil {
      return nil, err
    }
    deviceconfig.AdditionalInfo = CreateDeviceConfigReq.AdditionalInfo
  ```

- First()方法查不到会报错，可判断errors.Is(err, gorm.ErrRecordNotFound)
- 缓存相关查看initialize/缓存说明.md

## 开发中的一些说明和规范

### 基本功能的路径命名和请求方法

- **新增资源**
  - 路径：`POST /resources` 
  - 使用`POST`方法，不添加`/create`后缀，将新资源的数据包含在请求体中。
- **查看详情**
  - 路径：`GET /resources/{id}`
  - 使用`GET`方法获取特定资源的详细信息，资源ID通过路径参数传递。
- **编辑/更新资源**
  - 路径：`PUT /resources/{id}` 
  - 使用`PUT`方法更新资源，将更新的数据包含在请求体中。路径参数中传递资源ID。
- **删除资源**
  - 路径：`DELETE /resources/{id}`
  - 使用`DELETE`方法删除指定的资源，资源ID通过路径参数传递。
- **获取资源列表（支持过滤、排序）**
  - 路径：`GET /resources`
  - 使用`GET`方法检索资源列表，支持通过查询参数进行过滤和排序。

### 请求数据传递

- **查询参数**：适用于`GET`请求，用于过滤、排序或指定返回的数据格式等。例如，`GET /resources?role=admin&page=2&limit=10`。
- **路径参数**：用于指定资源的请求（如`GET`、`DELETE`、`PUT`），标识特定资源。例如，`DELETE /resources/{id}`。
- **请求体**：适用于需要传递复杂数据的`POST`和`PUT`请求，请求体可以是JSON、XML等格式。

### 返回数据

- `POST`请求返回新创建的资源的完整表示。这包括由服务器端生成的任何属性（如ID、创建时间等），以确保客户端具有最新的资源状态。
- `PUT`返回更新后的资源的完整表示。即使客户端可能已提供完整的资源表示，返回更新后的状态也有助于确认更新的结果，包括任何由服务器自动修改的字段（如修改时间）。

### 复杂查询与原生SQL

- 对于复杂的查询逻辑，可以使用原生SQL编写以提高维护效率和代码的清晰度。这种方法特别适合于难以通过ORM工具直接实现的复杂查询需求。在代码库中，可参考具体实现方法，如`dal/ota_upgrade_tasks.go`的`GetOtaUpgradeTaskListByPage`方法，来看如何在实践中应用。

### 提交代码规范

>`提交信息应使用英语撰写`

- Feat：新功能
- Fix：修补bug
- Docs：文档（documentation）
- Style： 格式（不影响代码运行的变动）
- Refactor：重构（即不是新增功能，也不是修改bug的代码变动）
- Test：增加测试
- Chore：构建过程或辅助工具的变动

## 虽然大家都知道

`golang的核心原则和哲学`

- 简洁性（"Less is exponentially more"）: 这是 Rob Pike 在其著名的博客文章中提到的。他强调，通过减少语言中的元素数量，可以实现更大的表达力和灵活性。

- 高效的并发（"Concurrency is not parallelism"）: Pike 在这方面强调区分并发（Concurrency）和并行（Parallelism）。Go 语言的并发模型（goroutines 和 channels）被设计为一种有效管理并发操作的手段，而不仅仅是实现多核并行计算。

- 清晰的错误处理（"Clear is better than clever"）: 这是 Go 语言中一个核心理念，强调代码的清晰性和可读性。例如，Go 避免使用传统的异常处理机制，而是采用显式的错误检查，使得错误处理更加清晰和直接。

- 代码的统一格式（"Gofmt's style is no one's favorite, yet gofmt is everyone's favorite"）: Go 引入了 gofmt 工具，强制统一的代码格式。这样做的目的是消除关于代码格式的争论，使所有 Go 代码看起来都一致，提高可读性。

- 避免隐藏的复杂性（"Do less. Enable more."）: Go 设计的另一个核心理念是**避免过度抽象和隐藏的复杂性**。Go 语言鼓励直接的解决方案，而不是那些表面上看起来很聪明，但实际上可能隐藏了复杂性和潜在问题的方法。

- 工具和社区（"The bigger the interface, the weaker the abstraction"）: 这个原则是关于接口设计的，它鼓励**创建小而专注的接口，而不是大而全能的接口**。这与 Go 社区和工具链的设计理念是一致的，即提供简单、有效的工具，以及鼓励社区贡献和合作。

## Swagger说明

```bash
# 在项目根目录执行
swag init
```

- 访问 Swagger UI
启动服务后，访问：<http://localhost:9999/swagger/index.html>
- 编写规范
参考接口/api/v1/login [post]

# golang约定

## golang-standards

**参考仓库**：https://github.com/golang-standards

**文档**：https://github.com/golang-standards/project-layout/blob/master/README_zh.md

1. **正面评价**：许多开发者赞赏它提供的结构清晰和组织良好的项目布局。特别是对于新手和从其他编程语言转到 Go 的开发者来说，这些指南可以作为一个很好的起点，帮助他们快速上手并遵循一些普遍接受的最佳实践。

2. **批评意见**：一些高级 Go 开发者指出，这些结构可能过于复杂或不必要，特别是对于小型或简单的项目。Go 的哲学是鼓励简单和直接，而 project-layout 提供的模板可能会导致过度的结构化和不必要的复杂性。

本项目只在部分参考project-layout

## golang的核心原则和哲学

- 简洁性（"Less is exponentially more"）: 这是 Rob Pike 在其著名的博客文章中提到的。他强调，通过减少语言中的元素数量，可以实现更大的表达力和灵活性。

- 高效的并发（"Concurrency is not parallelism"）: Pike 在这方面强调区分并发（Concurrency）和并行（Parallelism）。Go 语言的并发模型（goroutines 和 channels）被设计为一种有效管理并发操作的手段，而不仅仅是实现多核并行计算。

- 清晰的错误处理（"Clear is better than clever"）: 这是 Go 语言中一个核心理念，强调代码的清晰性和可读性。例如，Go 避免使用传统的异常处理机制，而是采用显式的错误检查，使得错误处理更加清晰和直接。

- 代码的统一格式（"Gofmt's style is no one's favorite, yet gofmt is everyone's favorite"）: Go 引入了 gofmt 工具，强制统一的代码格式。这样做的目的是消除关于代码格式的争论，使所有 Go 代码看起来都一致，提高可读性。

- 避免隐藏的复杂性（"Do less. Enable more."）: Go 设计的另一个核心理念是**避免过度抽象和隐藏的复杂性**。Go 语言鼓励直接的解决方案，而不是那些表面上看起来很聪明，但实际上可能隐藏了复杂性和潜在问题的方法。

- 工具和社区（"The bigger the interface, the weaker the abstraction"）: 这个原则是关于接口设计的，它鼓励**创建小而专注的接口，而不是大而全能的接口**。这与 Go 社区和工具链的设计理念是一致的，即提供简单、有效的工具，以及鼓励社区贡献和合作。


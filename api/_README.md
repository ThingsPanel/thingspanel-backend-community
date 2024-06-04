# 注意事项

1. 不使用gin的ShouldBindJSON来做校验，一律使用ValidateStruct函数校验
   1. ShouldBindJSON无法对指针类型做出合理处理，它也并非专注对结构体的校验
   2. 这里用"github.com/go-playground/validator/v10"包来校验
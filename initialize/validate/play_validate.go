package validate

// import (
// 	"errors"

// 	"github.com/go-playground/locales/zh"
// 	ut "github.com/go-playground/universal-translator"
// 	"github.com/go-playground/validator/v10"
// 	zh_translations "github.com/go-playground/validator/v10/translations/zh"
// )

// func InitValidator() (*validator.Validate, ut.Translator, error) {
// 	// 创建 validate 实例
// 	validate := validator.New()

// 	// 创建中文翻译器
// 	zh := zh.New()
// 	uni := ut.New(zh, zh)

// 	// 获取翻译器
// 	trans, err := uni.GetTranslator("zh")
// 	if !err {
// 		return nil, nil, errors.New("翻译器创建失败")
// 	}

// 	// 注册翻译器
// 	if err := zh_translations.RegisterDefaultTranslations(validate, trans); err != nil {
// 		return nil, nil, err
// 	}

// 	// 自定义错误消息
// 	if err := validate.RegisterTranslation("required", trans, func(ut ut.Translator) error {
// 		return ut.Add("required", "{0} 为必填项", true)
// 	}, func(ut ut.Translator, fe validator.FieldError) string {
// 		t, _ := ut.T("required", fe.Field())
// 		return t
// 	}); err != nil {
// 		return nil, nil, err
// 	}

// 	return validate, trans, nil
// }

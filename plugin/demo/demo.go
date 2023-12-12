package demo

import (
	hook "ThingsPanel-Go/hook"
	"ThingsPanel-Go/models"
	"fmt"
)

type DemoPlugin struct{}

func (p *DemoPlugin) LoginAdditionalInfoVerifyHook(user *models.Users) error {
	fmt.Println("我是示例插件！")
	return nil
}

func Init() {
	fmt.Println("示例插件初始化")
	hook.RegisterHookFactory("demo", func() hook.Hook {
		return &DemoPlugin{}
	})
}

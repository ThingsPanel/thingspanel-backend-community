package cache

import (
	"fmt"

	"github.com/beego/beego/v2/client/cache"
)

var Bm cache.Cache
var Err error

func init() {
	Bm, Err = cache.NewCache("memory", `{"interval":60}`)
	if Err != nil {
		fmt.Println("初始化cache失败", Err)
	}
}

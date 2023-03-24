package tp_log

import (
	"fmt"
	"log"
	"time"

	"github.com/beego/beego/v2/core/logs"
	beego "github.com/beego/beego/v2/server/web"
)

func init() {
	// go基本log设置
	log.SetFlags(log.Lshortfile | log.Ltime | log.Ldate)
	log.Println("系统日志初始化...")
	//beego日志模块配置
	dateStr := time.Now().Format("2006-01-02")
	maxdays, _ := beego.AppConfig.String("maxdays")
	level, _ := beego.AppConfig.String("level")
	maxlines, _ := beego.AppConfig.String("maxlines")
	dataSource := fmt.Sprintf(`{"filename":"files/logs/%s/log.log","level":%s,"maxlines":%s,"maxsize":0,"daily":true,"maxdays":%s,"color":true}`,
		dateStr,
		level,
		maxlines,
		maxdays,
	)
	levelInt, _ := beego.AppConfig.Int("level")
	logs.SetLevel(levelInt) // 如果不设置日志等级，logs.AdapterConsole适配器不生效
	//maxdays 文件最多保存多少天，默认保存 7 天
	logs.SetLogger(logs.AdapterFile, dataSource)
	// 输出log时能显示输出文件名和行号（非必须）
	logs.EnableFuncCallDepth(true)
	//异步输出
	logs.Async()
	log.Println("系统日志初始化完成")

}

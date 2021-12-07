package tcp

import (
	"ThingsPanel-Go/services"
	"fmt"
	"runtime/debug"
	"sync/atomic"
	"time"

	"github.com/fwhezfwhez/tcpx"
)

var requestTimes int32
var TSKVS services.TSKVService

func Listen(tcpPort string) {
	srv := tcpx.NewTcpX(nil)
	srv.UseGlobal(countRequestTime)
	srv.HandleRaw = func(c *tcpx.Context) {
		var buf = make([]byte, 81920)
		var n int
		var e error
		for {
			n, e = c.ConnReader.Read(buf)
			if e != nil {
				fmt.Println(e.Error())
				return
			}
			fmt.Println("receive:", string(buf[:n]))
			TSKVS.MsgProc(buf[:n])
		}
	}
	srv.ListenAndServeRaw("tcp", tcpPort)
	go func() {
		time.Sleep(1 * time.Second)
		if e := srv.Stop(false); e != nil {
			fmt.Println(fmt.Sprintf("%s \n %s", e.Error(), debug.Stack()))
		}
	}()
}

func countRequestTime(c *tcpx.Context) {
	atomic.AddInt32(&requestTimes, 1)
}

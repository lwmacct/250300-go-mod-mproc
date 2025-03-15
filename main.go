package main

import (
	"time"

	"github.com/lwmacct/250300-go-mod-mproc/pkg/mproc"
)

func main() {
	netdev, err := mproc.NewNetDev("all", 1*time.Second)
	if err != nil {
		panic(err)
	}

	// 暂停 30 秒
	time.Sleep(30 * time.Second)

	// 关闭 netdev
	netdev.Close()
}

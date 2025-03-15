package mproc

import (
	"bufio"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/lwmacct/250300-go-mod-mlog/pkg/mlog"
	"github.com/lwmacct/250300-go-mod-pkgs/pkg/mfunc"
	"github.com/lwmacct/250300-go-mod-pkgs/pkg/mto"
)

type netDev struct {
	args *netDevArgs
	done chan struct{} // 用于信号goroutine退出的通道
}

type netDevArgs struct {
	Name     string        // 名称, 会设置到 CallData 的 Name 字段
	Interval time.Duration // 采样间隔

	Callback   func(data TsCallData) // 保存数据的回调函数
	Interfaces []string              // 需要监控的接口
	Path       string                // 网络设备文件路径
}
type netDevOpts func(*netDev)

// NewNetDev 读取并解析网络设备文件
func NewNetDev(name string, interval time.Duration, opts ...netDevOpts) (*netDev, error) {
	t := &netDev{
		args: &netDevArgs{
			Name:       name,
			Interval:   interval,
			Interfaces: nil,
			Callback: func(data TsCallData) {
				mlog.Info(mlog.H{
					"name":       data.Name,
					"bytes_tx":   data.BytesTx,
					"bytes_rx":   data.BytesRx,
					"interfaces": data.Interfaces,
					"interval":   data.Interval,
				})
			},
			Path: "/proc/net/dev",
		},
		done: make(chan struct{}),
	}
	for _, opt := range opts {
		opt(t)
	}
	t.start()
	return t, nil
}

// WithPath 设置网络设备文件路径
func WithPath(path string) netDevOpts {
	return func(t *netDev) {
		t.args.Path = path
	}
}

// WithCallback 设置回调函数
func WithCallback(callback func(data TsCallData)) netDevOpts {
	return func(t *netDev) {
		t.args.Callback = callback
	}
}

// Close 关闭netDev并停止所有goroutine
func (t *netDev) Close() {
	close(t.done)
}

func (t *netDev) start() {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				mlog.Error(mlog.H{"error": "netDev goroutine panic", "reason": r})
			}
		}()

		for {
			select {
			case <-t.done:
				return // 收到关闭信号时退出goroutine
			default:
				t.calculate()
			}
		}
	}()
}

// 传入回调函数
func (n *netDev) calculate() {
	var lastRx, lastTx int64
	firstIteration := true // 是否为第一次迭代

	ticker := time.NewTicker(n.args.Interval) // 每 interval 执行一次
	defer ticker.Stop()

	for {
		select {
		case <-n.done:
			return // 收到关闭信号时退出
		case <-ticker.C:
			totalRx, totalTx := int64(0), int64(0)
			stats, err := n.readNetDev() // 获取当前所有接口的数据
			if err != nil {
				mlog.Error(mlog.H{"error": err.Error()})
				continue // 出错时继续下一次循环，而不是break
			}

			// 累加所有接口的接收和发送字节数
			for _, v := range stats {
				totalRx += v.Receive.Bytes
				totalTx += v.Transmit.Bytes
			}
			if !firstIteration {
				BytesRx := (totalRx - lastRx) / int64(n.args.Interval.Seconds())
				BytesTx := (totalTx - lastTx) / int64(n.args.Interval.Seconds())

				if BytesRx < 0 {
					BytesRx = 0
				}

				if BytesTx < 0 {
					BytesTx = 0
				}
				n.args.Callback(TsCallData{
					Name:       n.args.Name,
					BytesTx:    BytesTx,
					BytesRx:    BytesRx,
					Interval:   n.args.Interval,
					Interfaces: n.args.Interfaces,
				})
			} else {
				firstIteration = false
			}

			// 更新上一次的接收和发送总字节
			lastRx = totalRx
			lastTx = totalTx
		}
	}
}

func (n *netDev) readNetDev() (map[string]tsNetDev, error) {
	file, err := os.Open(n.args.Path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	items := make(map[string]tsNetDev)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.Contains(line, ":") {
			continue
		}

		fields := strings.Fields(line)
		ifname := strings.Trim(fields[0], ":")

		if n.args.Interfaces != nil {
			if !slices.Contains(n.args.Interfaces, ifname) {
				continue
			}
		}

		count := mfunc.NewCounter(1)
		iface := tsNetDev{
			Name: ifname,
			Receive: tsNetDevInfo{
				Bytes:      mto.Int64(fields[count()]),
				Packets:    mto.Int64(fields[count()]),
				Errs:       mto.Int64(fields[count()]),
				Drop:       mto.Int64(fields[count()]),
				FIFO:       mto.Int64(fields[count()]),
				Frame:      mto.Int64(fields[count()]),
				Compressed: mto.Int64(fields[count()]),
				Multicast:  mto.Int64(fields[count()]),
			},
			Transmit: tsNetDevInfo{
				Bytes:      mto.Int64(fields[count()]),
				Packets:    mto.Int64(fields[count()]),
				Errs:       mto.Int64(fields[count()]),
				Drop:       mto.Int64(fields[count()]),
				FIFO:       mto.Int64(fields[count()]),
				Colls:      mto.Int64(fields[count()]),
				Carrier:    mto.Int64(fields[count()]),
				Compressed: mto.Int64(fields[count()]),
			},
		}
		items[ifname] = iface
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

type TsCallData struct {
	BytesTx int64
	BytesRx int64

	Interval   time.Duration
	Interfaces []string

	Name string
}

type tsNetDev struct {
	Name     string       `json:"name"`
	Transmit tsNetDevInfo `json:"transmit"`
	Receive  tsNetDevInfo `json:"receive"`
}

type tsNetDevInfo struct {
	Bytes      int64 `json:"bytes"`
	Packets    int64 `json:"packets"`
	Errs       int64 `json:"errs"`
	Drop       int64 `json:"drop"`
	FIFO       int64 `json:"fifo"`
	Frame      int64 `json:"frame,omitempty"`
	Compressed int64 `json:"compressed"`
	Multicast  int64 `json:"multicast,omitempty"`
	Colls      int64 `json:"colls,omitempty"`
	Carrier    int64 `json:"carrier,omitempty"`
}

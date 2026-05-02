package stream

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"time"
)

// StreamDataIntervalTimeout 流式数据相邻 chunk 的最大间隔超时
// 上游 Claude/控制台在此时间内未发送任何新数据时，主动关闭连接释放资源
const StreamDataIntervalTimeout = 180 * time.Second

// StreamWriteDeadline 单次写下游的最大耗时
// 防止恶意客户端不读 socket 导致服务端 Write 永久阻塞 (慢速读取 DoS)
const StreamWriteDeadline = 90 * time.Second

// ReadWithIntervalTimeout 带间隔超时的流式读取
// 任意一次 Read 阻塞超过 timeout，则关闭 closer 让阻塞的 Read 返回，避免 goroutine 泄漏
// ctx 取消时同样关闭 closer 并返回 ctx.Err()
func ReadWithIntervalTimeout(ctx context.Context, reader *bufio.Reader, buf []byte, timeout time.Duration, closer io.Closer) (int, error) {
	type result struct {
		n   int
		err error
	}
	ch := make(chan result, 1)
	go func() {
		n, err := reader.Read(buf)
		ch <- result{n, err}
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case r := <-ch:
		return r.n, r.err
	case <-timer.C:
		if closer != nil {
			_ = closer.Close()
		}
		<-ch // 等待 goroutine 退出，避免泄漏
		return 0, fmt.Errorf("流式数据间隔超时（%s 内无新数据）", timeout)
	case <-ctx.Done():
		if closer != nil {
			_ = closer.Close()
		}
		<-ch
		return 0, ctx.Err()
	}
}

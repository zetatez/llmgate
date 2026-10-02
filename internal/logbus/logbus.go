// Package logbus 提供请求日志的异步批量落库通道。
package logbus

import (
	"database/sql"
	"log"
	"sync"
	"time"

	"llmgate/internal/models"
	"llmgate/internal/store"
)

const flushBatchSize = 100

// Bus 异步接收请求日志并批量写入 SQLite，避免高频写阻塞网关。
type Bus struct {
	ch      chan *models.RequestLog
	db      *sql.DB
	stop    chan struct{}
	wg      sync.WaitGroup
	dropped int64
}

// New 创建日志总线并启动后台协程。
func New(db *sql.DB, buffer int) *Bus {
	if buffer <= 0 {
		buffer = 1024
	}
	b := &Bus{ch: make(chan *models.RequestLog, buffer), db: db, stop: make(chan struct{})}
	b.wg.Add(1)
	go b.run()
	return b
}

// Write 投递一条日志；缓冲满时丢弃（保证不阻塞网关）。
func (b *Bus) Write(l *models.RequestLog) {
	select {
	case b.ch <- l:
	default:
		b.dropped++
		if b.dropped%100 == 1 {
			log.Printf("logbus: buffer full, dropped=%d", b.dropped)
		}
	}
}

// run 批量刷盘：积攒到 100 条或 1 秒定时。
func (b *Bus) run() {
	defer b.wg.Done()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	batch := make([]*models.RequestLog, 0, flushBatchSize)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		if err := store.InsertLogs(b.db, batch); err != nil {
			log.Printf("logbus: insert failed: %v (dropped %d logs)", err, len(batch))
		}
		batch = batch[:0]
	}
	defer flush()
	for {
		select {
		case l := <-b.ch:
			batch = append(batch, l)
			if len(batch) >= flushBatchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-b.stop:
			// 收尾前先排空缓冲区中尚未消费的日志，避免停机丢失在途审计日志。
			// 通道有界且默认非阻塞写，用非阻塞读排到空再刷盘 + 返回，不会阻塞。
			for {
				select {
				case l := <-b.ch:
					batch = append(batch, l)
					if len(batch) >= flushBatchSize {
						flush()
					}
				default:
					flush()
					return
				}
			}
		}
	}
}

// Close 停止后台协程并做最后一次刷盘。
func (b *Bus) Close() {
	close(b.stop)
	b.wg.Wait()
}

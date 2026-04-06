/*
 * Copyright 2024 CloudWeGo Authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package pdf

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// PDFRenderPool PDF 渲染资源池，用于控制并发渲染任务，防止资源耗尽
// 适用于多用户多租户场景
type PDFRenderPool struct {
	semaphore   chan struct{}
	mu          sync.Mutex
	activeCount int32 // 当前活跃任务数
	totalCount  int64 // 累计完成任务数
}

// PoolConfig 渲染池配置
type PoolConfig struct {
	MaxConcurrent int           // 最大并发渲染数，默认 5
	QueueTimeout  time.Duration // 队列等待超时时间，默认 30 秒
}

// 全局默认渲染池（单例）
var defaultRenderPool *PDFRenderPool

func init() {
	defaultRenderPool = NewPDFRenderPool(PoolConfig{
		MaxConcurrent: 5,
		QueueTimeout:  30 * time.Second,
	})
}

// NewPDFRenderPool 创建新的 PDF 渲染资源池
func NewPDFRenderPool(config PoolConfig) *PDFRenderPool {
	if config.MaxConcurrent <= 0 {
		config.MaxConcurrent = 5
	}
	if config.QueueTimeout <= 0 {
		config.QueueTimeout = 30 * time.Second
	}

	return &PDFRenderPool{
		semaphore: make(chan struct{}, config.MaxConcurrent),
	}
}

// GetDefaultPool 获取全局默认渲染池
func GetDefaultPool() *PDFRenderPool {
	return defaultRenderPool
}

// Execute 在渲染池中执行一个任务
// 如果池已满，会等待直到有空闲槽位或超时
func (p *PDFRenderPool) Execute(ctx context.Context, fn func(ctx context.Context) error) error {
	// 尝试获取信号量槽位
	select {
	case p.semaphore <- struct{}{}:
		// 获取成功，确保最终释放
		defer func() { <-p.semaphore }()
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(p.getQueueTimeout()):
		active := atomic.LoadInt32(&p.activeCount)
		return fmt.Errorf("pdf render pool: queue timeout (active tasks: %d)", active)
	}

	// 更新计数
	atomic.AddInt32(&p.activeCount, 1)
	defer atomic.AddInt32(&p.activeCount, -1)
	defer atomic.AddInt64(&p.totalCount, 1)

	// 执行任务
	return fn(ctx)
}

// ExecuteWithImages 在渲染池中执行 PDF 转图片任务
func (p *PDFRenderPool) ExecuteWithImages(ctx context.Context, pdfPath string, opts *ExtractPDFOptions) ([]PDFPageImage, error) {
	var result []PDFPageImage
	err := p.Execute(ctx, func(ctx context.Context) error {
		var err error
		result, err = ExtractPDFPagesAsImages(ctx, pdfPath, opts)
		return err
	})
	return result, err
}

// GetStats 获取渲染池统计信息
func (p *PDFRenderPool) GetStats() PoolStats {
	return PoolStats{
		MaxConcurrent: cap(p.semaphore),
		ActiveTasks:   int(atomic.LoadInt32(&p.activeCount)),
		Available:     cap(p.semaphore) - int(atomic.LoadInt32(&p.activeCount)),
		TotalCompleted: atomic.LoadInt64(&p.totalCount),
	}
}

// PoolStats 渲染池统计信息
type PoolStats struct {
	MaxConcurrent  int   `json:"max_concurrent"`   // 最大并发数
	ActiveTasks    int   `json:"active_tasks"`     // 当前活跃任务数
	Available      int   `json:"available"`        // 可用槽位数
	TotalCompleted int64 `json:"total_completed"`  // 累计完成任务数
}

func (p *PDFRenderPool) getQueueTimeout() time.Duration {
	// 可以从配置读取，这里使用默认值
	return 30 * time.Second
}

// SetGlobalPool 设置全局默认渲染池
// 注意：应在服务启动时调用，运行时调用可能导致竞态条件
func SetGlobalPool(config PoolConfig) {
	defaultRenderPool = NewPDFRenderPool(config)
}

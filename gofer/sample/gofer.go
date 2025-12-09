// Package sample 实现了一个线程池（goroutine池）用于管理和复用goroutine
package sample

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-leo/goconc/gofer"
)

// 定义线程池相关的错误类型
var (
	ErrPoolFull        = errors.New("gofer: pool is full")   // 线程池已满
	ErrPoolClosed      = errors.New("gofer: pool is closed") // 线程池已关闭
	ErrTaskNil         = errors.New("gofer: task is nil")    // 任务为空
	ErrPoolSizeInvalid = errors.New("gofer: maximumPoolSize must be greater than or equal to corePoolSize")
)

var _ gofer.Gofer = (*Gofer)(nil)

// 定义线程池关闭状态的常量
const stateClosed = -1

// options 线程池配置选项
type options struct {
	// 核心线程数，即使线程处于空闲状态，也不会被回收
	CorePoolSize int
	// 线程池所能容纳的最大线程数
	MaximumPoolSize int
	// 非核心线程闲置时的存活时间
	KeepAliveTime time.Duration
	// 任务队列，用于存放等待执行的任务
	WorkQueue chan func()
	// 错误处理函数，用于处理任务执行过程中的panic
	Recover func(p any, stack []byte)
}

// Option 配置选项函数类型
type Option func(*options)

// CorePoolSize 设置核心线程数
func CorePoolSize(size int) Option {
	return func(o *options) {
		o.CorePoolSize = size
	}
}

// MaximumPoolSize 设置最大线程数
func MaximumPoolSize(size int) Option {
	return func(o *options) {
		o.MaximumPoolSize = size
	}
}

// KeepAliveTime 设置非核心线程的存活时间
func KeepAliveTime(dur time.Duration) Option {
	return func(o *options) {
		o.KeepAliveTime = dur
	}
}

// WorkQueue 设置工作队列
func WorkQueue(q chan func()) Option {
	return func(o *options) {
		o.WorkQueue = q
	}
}

// Recover 设置错误处理函数
func Recover(f func(p any, stack []byte)) Option {
	return func(o *options) {
		o.Recover = f
	}
}

// Apply 应用配置选项
func (o *options) Apply(opts ...Option) *options {
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// Correct 校正配置参数，设置默认值
func (o *options) Correct() *options {
	if o.CorePoolSize <= 0 {
		o.CorePoolSize = runtime.NumCPU() // 默认核心线程数为CPU核心数
	}
	if o.MaximumPoolSize < o.CorePoolSize {
		o.MaximumPoolSize = 2 * o.CorePoolSize // 默认最大线程数为核心线程数的2倍
	}
	if o.KeepAliveTime <= 0 {
		o.KeepAliveTime = 300 * time.Second // 默认存活时间为300秒
	}
	if o.WorkQueue == nil {
		o.WorkQueue = make(chan func(), 1000*o.MaximumPoolSize) // 默认队列容量为最大线程数的1000倍
	}
	if o.Recover == nil {
		// 默认的错误处理函数，打印panic信息和堆栈
		o.Recover = func(p any, stack []byte) {
			fmt.Printf("gofer: panic trigger, %v, stack: %s", p, stack)
		}
	}
	return o
}

// New 创建一个新的线程池实例
func New(opts ...Option) *Gofer {
	options := new(options).Apply(opts...).Correct()
	return &Gofer{
		options: options,
		closeC:  make(chan struct{}), // 用于通知关闭的channel
	}
}

// Gofer 线程池结构体
type Gofer struct {
	options *options       // 配置选项
	m       sync.Mutex     // 互斥锁，保护状态变更
	wg      sync.WaitGroup // 等待组，等待所有任务完成
	state   int32          // 当前运行的线程数，-1表示已关闭
	closeC  chan struct{}  // 关闭通知channel
}

// Go 提交一个任务到线程池执行
func (g *Gofer) Go(task func()) error {
	// 检查任务是否为空
	if task == nil {
		return ErrTaskNil
	}

	// 检查线程池是否已关闭（无锁检查）
	state := atomic.LoadInt32(&g.state)
	if state == stateClosed {
		return ErrPoolClosed
	}

	// 检查是否超过最大线程数（无锁检查）
	if int(state) >= g.options.MaximumPoolSize {
		return ErrPoolFull
	}

	// 加锁进行精确检查和状态变更
	g.m.Lock()
	defer g.m.Unlock()
	state = atomic.LoadInt32(&g.state)
	if state == stateClosed {
		return ErrPoolClosed
	}

	// 再次检查是否超过最大线程数
	if int(state) >= g.options.MaximumPoolSize {
		return ErrPoolFull
	}

	// 当提交一个新任务时，线程池会判断当前运行的线程数是否小于corePoolSize，如果小于，则创建新线程执行任务
	if int(state) < g.options.CorePoolSize {
		newWorker := &worker{
			Options:   g.options,
			Gofer:     g,
			Core:      true, // 标记为核心线程
			FirstTask: task, // 第一个要执行的任务
		}
		newWorker.work() // 启动工作线程
		return nil
	}

	// 尝试将任务放入工作队列
	select {
	case g.options.WorkQueue <- task:
		// 如果运行的线程数等于或大于corePoolSize，则将任务加入workQueue等待执行
		return nil
	default:
		// 如果workQueue已满，且当前运行的线程数小于maximumPoolSize，则创建新线程执行任务
		newWorker := &worker{
			Options:   g.options,
			Gofer:     g,
			Core:      false, // 标记为非核心线程
			FirstTask: task,  // 第一个要执行的任务
		}
		newWorker.work() // 启动工作线程
		return nil
	}
}

// Close 关闭线程池，等待所有任务完成
func (g *Gofer) Close(ctx context.Context) error {
	// 检查线程池是否已关闭（无锁检查）
	if atomic.LoadInt32(&g.state) == stateClosed {
		return ErrPoolClosed
	}

	// 加锁进行精确检查和状态变更
	g.m.Lock()
	defer g.m.Unlock()
	if atomic.LoadInt32(&g.state) == stateClosed {
		return ErrPoolClosed
	}

	// 设置关闭状态并关闭通知channel
	atomic.StoreInt32(&g.state, stateClosed)
	close(g.closeC)

	// 创建等待完成的channel
	waitC := make(chan struct{})
	go func() {
		g.wg.Wait()  // 等待所有任务完成
		close(waitC) // 关闭等待channel
	}()

	// 等待关闭完成或上下文取消
	select {
	case <-ctx.Done():
		return ctx.Err() // 上下文取消，返回取消错误
	case <-waitC:
		return nil // 正常关闭完成
	}
}

// worker 工作线程结构体
type worker struct {
	Options   *options // 线程池配置
	Gofer     *Gofer   // 所属的线程池
	Core      bool     // 是否为核心线程
	FirstTask func()   // 第一个要执行的任务
}

// work 启动工作线程
func (w *worker) work() {
	// 增加等待组计数和线程池状态计数
	w.Gofer.wg.Add(1)
	atomic.AddInt32(&w.Gofer.state, 1)

	// 启动goroutine执行任务
	go func() {
		// 任务完成后减少等待组计数和线程池状态计数
		defer w.Gofer.wg.Done()
		defer atomic.AddInt32(&w.Gofer.state, -1)

		// 捕获任务执行过程中的panic
		defer func() {
			if r := recover(); r != nil {
				// 调用自定义的错误处理函数
				w.Options.Recover(r, debug.Stack())
			}
		}()

		// 执行第一个任务
		w.FirstTask()

		// 根据是否为核心线程选择不同的任务处理逻辑
		if w.Core {
			// 核心线程：持续从工作队列获取任务执行，直到线程池关闭
			for {
				select {
				case task, ok := <-w.Options.WorkQueue:
					if !ok {
						return
					}
					if task == nil {
						return
					}
					task()
				case <-w.Gofer.closeC:
					return
				}
			}
		} else {
			// 非核心线程：持续从工作队列获取任务执行，直到线程池关闭或超时
			for {
				select {
				case task, ok := <-w.Options.WorkQueue:
					if !ok {
						return
					}
					if task == nil {
						return
					}
					task()
				case <-w.Gofer.closeC:
					return
				case <-time.After(w.Options.KeepAliveTime):
					// 非核心线程超时后退出
					return
				}
			}
		}
	}()
}

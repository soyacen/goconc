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
	"github.com/go-leo/goconc/waiter"
)

var (
	ErrPoolFull   = errors.New("gofer: pool is full")
	ErrPoolClosed = errors.New("gofer: pool is closed")
	ErrTaskNil    = errors.New("gofer: task is nil")
)

var _ gofer.Gofer = (*Gofer)(nil)

type options struct {
	CorePoolSize int

	MaximumPoolSize int

	KeepAliveTime time.Duration

	WorkQueueSize int

	Recover func(p any, stack []byte)
}

type Option func(*options)

func CorePoolSize(size int) Option {
	return func(o *options) {
		o.CorePoolSize = size
	}
}

func MaximumPoolSize(size int) Option {
	return func(o *options) {
		o.MaximumPoolSize = size
	}
}

func KeepAliveTime(dur time.Duration) Option {
	return func(o *options) {
		o.KeepAliveTime = dur
	}
}

func WorkQueueSize(size int) Option {
	return func(o *options) {
		o.WorkQueueSize = size
	}
}

func Recover(f func(p any, stack []byte)) Option {
	return func(o *options) {
		o.Recover = f
	}
}

func (o *options) Apply(opts ...Option) *options {
	for _, opt := range opts {
		opt(o)
	}
	return o
}

func (o *options) Correct() *options {
	if o.CorePoolSize <= 0 {
		o.CorePoolSize = 256
	}
	if o.MaximumPoolSize < o.CorePoolSize {
		o.MaximumPoolSize = runtime.GOMAXPROCS(0) * o.CorePoolSize
	}
	if o.KeepAliveTime <= 0 {
		o.KeepAliveTime = 5 * time.Minute
	}
	if o.WorkQueueSize < 0 {
		o.WorkQueueSize = runtime.NumCPU() * o.MaximumPoolSize
	}
	if o.Recover == nil {
		o.Recover = func(p any, stack []byte) {
			fmt.Printf("gofer: panic trigger, %v, stack: %s", p, stack)
		}
	}
	return o
}

func New(opts ...Option) *Gofer {
	options := new(options).Apply(opts...).Correct()
	return &Gofer{
		options:     options,
		coreWorkers: make(map[*coreWorker]struct{}, options.CorePoolSize),
		edgeWorkers: make(map[*edgeWorker]struct{}, options.MaximumPoolSize-options.CorePoolSize),
		WorkQueue:   make(chan func(), options.WorkQueueSize),
	}
}

type Gofer struct {
	options     *options
	m           sync.Mutex
	wg          sync.WaitGroup
	coreWorkers map[*coreWorker]struct{}
	edgeWorkers map[*edgeWorker]struct{}
	WorkQueue   chan func()
	closed      atomic.Bool
}

func (g *Gofer) Go(task func()) error {
	if task == nil {
		return ErrTaskNil
	}
	if g.closed.Load() {
		return ErrPoolClosed
	}
	g.m.Lock()
	defer g.m.Unlock()
	if g.closed.Load() {
		return ErrPoolClosed
	}
	if len(g.coreWorkers) < g.options.CorePoolSize {
		worker := &coreWorker{Gofer: g}
		g.coreWorkers[worker] = struct{}{}
		worker.work()
	} else if len(g.edgeWorkers) < g.options.MaximumPoolSize-g.options.CorePoolSize {
		worker := &edgeWorker{Gofer: g}
		g.edgeWorkers[worker] = struct{}{}
		worker.work()
	}
	job := func() {
		defer func() {
			if p := recover(); p != nil {
				g.options.Recover(p, debug.Stack())
			}
		}()
		task()
	}
	select {
	case g.WorkQueue <- job:
		return nil
	default:
		return ErrPoolFull
	}
}

func (g *Gofer) Close(ctx context.Context) error {
	if g.closed.Load() {
		return ErrPoolClosed
	}
	g.m.Lock()
	if g.closed.Load() {
		g.m.Unlock()
		return ErrPoolClosed
	}
	g.closed.Store(true)
	close(g.WorkQueue)
	g.m.Unlock()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-waiter.WaitNotify(&g.wg):
		return nil
	}
}

type coreWorker struct {
	Gofer *Gofer
}

func (w *coreWorker) work() {
	w.Gofer.wg.Add(1)
	go func() {
		defer w.Gofer.wg.Done()
		for job := range w.Gofer.WorkQueue {
			job()
		}
	}()
}

type edgeWorker struct {
	Gofer *Gofer
}

func (w *edgeWorker) work() {
	w.Gofer.wg.Add(1)
	go func() {
		defer w.Gofer.wg.Done()
		ticker := time.NewTicker(w.Gofer.options.KeepAliveTime)
		defer ticker.Stop()
		for {
			select {
			case job, ok := <-w.Gofer.WorkQueue:
				if !ok {
					return
				}
				job()
				ticker.Reset(w.Gofer.options.KeepAliveTime)
			case <-ticker.C:
				w.Gofer.m.Lock()
				delete(w.Gofer.edgeWorkers, w)
				w.Gofer.m.Unlock()
				return
			}
		}
	}()
}

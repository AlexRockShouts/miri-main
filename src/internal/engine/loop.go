package engine

import (
	"context"
	"sync"
	"time"
)

type Loop struct {
	mu     sync.Mutex
	funcs  []func()
	ticker *time.Ticker
	ctx    context.Context
	cancel context.CancelFunc
}

func New() *Loop {
	return &Loop{}
}

func (l *Loop) Register(fn func()) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.funcs = append(l.funcs, fn)
}

func (l *Loop) Start(ctx context.Context) {
	l.ctx, l.cancel = context.WithCancel(ctx)
	l.ticker = time.NewTicker(10 * time.Second)
	go l.run()
}

func (l *Loop) run() {
	defer l.ticker.Stop()
	for {
		select {
		case <-l.ctx.Done():
			return
		case <-l.ticker.C:
			l.mu.Lock()
			fs := make([]func(), len(l.funcs))
			copy(fs, l.funcs)
			l.mu.Unlock()
			for _, f := range fs {
				go f()
			}
		}
	}
}

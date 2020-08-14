package main

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestPoolReserve(t *testing.T) {
	t.Run("shutdown to open", func(t *testing.T) {
		p := spawnPool(StateShutdown)

		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			if err := p.reserve(); err != nil {
				t.Error(err)
			}
			wg.Done()
		}()

		waitReserve(p)

		p.broadcast(StateOpen)

		wg.Wait()

		if p.open != 1 {
			t.Error("reserve must be successful")
		}
	})

	t.Run("shutdown to close", func(t *testing.T) {
		p := spawnPool(StateShutdown)

		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			// expected state transition: shutdown -> closed -> starting -> closed
			_ = p.reserve()
			p.m.Lock()
			if p.open != 0 || p.state != StateClosed {
				t.Error("reserve must be unsuccessful due to failed start")
			}
			p.m.Unlock()
			wg.Done()
		}()

		waitReserve(p)

		p.broadcast(StateClosed)

		wg.Wait()
	})

	t.Run("shutdown to shutdown", func(t *testing.T) {
		p := spawnPool(StateShutdown)

		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			// expected state transition: shutdown -> ... -> shutdown
			if err := p.reserve(); err != ErrUnavailableRetry {
				t.Error(err)
			}
			p.m.Lock()
			if p.open != 0 || p.state != StateShutdown {
				t.Error("reserve must be unsuccessful")
			}
			p.m.Unlock()
			wg.Done()
		}()

		waitReserve(p)

		p.broadcast(StateShutdown)

		wg.Wait()
	})

	t.Run("shutdown to starting", func(t *testing.T) {
		p := spawnPool(StateShutdown)

		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			// expected state transition: shutdown -> starting -> open
			if err := p.reserve(); err != nil {
				t.Error(err)
			}
			p.m.Lock()
			if p.open == 0 || p.state != StateOpen {
				t.Error("reserve must be successful")
			}
			p.m.Unlock()
			wg.Done()
		}()

		waitReserve(p)

		p.broadcast(StateStarting)

		time.Sleep(time.Millisecond)

		p.m.Lock()
		if p.open == 0 || p.state != StateStarting {
			t.Error("reserve must be waiting for starting resources")
		}
		p.m.Unlock()

		p.broadcast(StateOpen)

		wg.Wait()
	})
}

func spawnPool(state int) *pool {
	p := &pool{
		state: state,
		last:  time.Now(),
		res: nil,
		cfg: ProcessPoolConfig{
			Ctx: context.Background(),
		},
	}

	p.c = sync.NewCond(&p.m)

	return p
}

func waitReserve(p *pool) {
	wait(func() bool {
		p.m.Lock()
		defer p.m.Unlock()
		return p.open > 0
	})
}

func wait(f func() bool) {
	for {
		if f() {
			break
		}
		time.Sleep(time.Microsecond)
	}
}

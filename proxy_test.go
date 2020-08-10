// +build integration

package main

import (
	"context"
	"log"
	"math/rand"
	"net"
	"testing"
	"time"
)

func TestLongRun(t *testing.T) {
	var buf [1]byte

	dest, err := net.ResolveTCPAddr("tcp", "127.0.0.1:3000")
	if err != nil {
		t.Fatal("resolve tcp address:", err)
	}

	p, err := NewServicePool(ProcessPoolConfig{
		Ctx:          context.Background(),
		Exec:         "/usr/sbin/ncat",
		Args:         []string{"-l", "-k", "-p", "3000", "-4", "-v", "--broker"},
		Dest:         dest,
		IdleTimeout:  0,
		StartTimeout: 200*time.Millisecond,
		ReadyHealthCheckInterval: time.Millisecond,
		Stats: true,
		StatsInterval: time.Second,
	})
	if err != nil {
		t.Fatal("service pool init:", err)
	}

	go func() {
		var ptr = p.(*pool)
		for {
			spawn := rand.Intn(10)
			for i := 0; i < spawn; i ++ {
				go func() {
					conn, err := net.Dial("tcp", "127.0.0.1:4000")
					if err != nil {
						log.Println(err)
					} else {
						if _, err = conn.Write(buf[:]); err != nil {
							log.Println(err)
						}

						_ = conn.Close()
					}
				}()
			}

			waitState(ptr, StateShutdown, StateClosed)
		}
	}()

	if err := NewProxy(p).Do("127.0.0.1:4000"); err != nil {
		t.Fatal("proxy:", err)
	}
}

func waitState(p *pool, state ...int) {
	wait(func() bool {
		p.m.Lock()
		defer p.m.Unlock()
		for _, t := range state {
			if p.state == t {
				return true
			}
		}
		return false
	})
}

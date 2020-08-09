package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"syscall"
	"time"
)

// Pool is a connection factory
type Pool interface {
	// Get acquires a new resource connection that must be returned later
	Get() (net.Conn, error)

	// Put releases a resource connection
	Put(net.Conn)
}

const (
	StateOpen = 0
	StateStarting = 1
	StateShutdown = 2
	StateClosed = 3
)

var ErrUnavailableRetry = errors.New("resource unavailable: retry")

type pool struct {
	// m synchronizes internal accesses and serves as a Locker for c
	m sync.Mutex

	// c is a communication channel for the coroutines that watch state change
	c *sync.Cond

	// state reflects current pool state
	state int

	// open counts the number of live connections
	open uint64

	// last shows the time point when the last Get/Put was called
	last time.Time

	// res is a managed resource. its state is managed outside of the m.
	res Process

	// cfg is a pool configuration
	cfg ProcessPoolConfig
}

var _ Pool = (*pool)(nil)

func NewServicePool(config ProcessPoolConfig) (Pool, error) {
	proc, err := NewProcess(config.Ctx, config.WorkDir, config.Exec, config.Args...)
	if err != nil {
		return nil, fmt.Errorf("new process: %w", err)
	}

	p := &pool{
		state: StateClosed,
		last:  time.Now(),
		res: proc,
		cfg: config,
	}

	p.c = sync.NewCond(&p.m)

	go p.stats()

	log.Printf("pool configuration: %+v", config)

	return p, nil
}

type ProcessPoolConfig struct {
	Ctx     context.Context
	WorkDir string
	Exec    string
	Args    []string

	Dest *net.TCPAddr

	IdleTimeout time.Duration
	StartTimeout time.Duration
	ReadyHealthCheckInterval time.Duration
}

// Get acquires a new resource that must be returned later
func (n *pool) Get() (net.Conn, error) {
	if err := n.reserve(); err != nil {
		return nil, err
	}

	conn, err := net.DialTCP("tcp", nil, n.cfg.Dest)
	if err != nil {
		n.release()
		return nil, err
	}

	return conn, nil
}

// Put releases a resource
func (n *pool) Put(conn net.Conn) {
	_ = conn.Close()
	n.release()
}

func (n *pool) reserve() error {
	n.m.Lock()
	defer n.m.Unlock()

	n.open++
	n.last = time.Now()

iter:
	switch n.state {
	case StateOpen:
		// resource is ready to serve

	case StateStarting:
		// wait for the resource to start
		n.c.Wait()

		if n.state != StateOpen {
			n.open--
			return ErrUnavailableRetry
		}

	case StateShutdown:
		// wait for the resource to shutdown
		n.c.Wait()

		if n.state == StateShutdown {
			n.open--
			return ErrUnavailableRetry
		}

		goto iter

	case StateClosed:
		n.state = StateStarting
		go n.start()

		// wait for the resource to start
		n.c.Wait()

		if n.state != StateOpen {
			n.open--
			return ErrUnavailableRetry
		}

	default:
		log.Fatalln("unknown pool state:", n.state)
	}

	return nil
}

func (n *pool) release() {
	n.m.Lock()
	defer n.m.Unlock()

	// assert
	if n.open == 0 {
		log.Fatalln("n.open == 0 and closing")
	}

	n.open--
	n.last = time.Now()
}

func (n *pool) broadcast(state int) {
	n.m.Lock()
	n.state = state
	n.m.Unlock()

	n.c.Broadcast()
}

// start spawns a resource and switches the FSM state
func (n *pool) start() {
	defer func() {
		if err := recover(); err != nil {
			log.Println(fmt.Errorf("recover: start: new process: %v", err))
			n.broadcast(StateClosed)
		}
	}()

	proc, err := NewProcess(n.cfg.Ctx, n.cfg.WorkDir, n.cfg.Exec, n.cfg.Args...)
	if err != nil {
		log.Println("start: new process:", err)
		n.broadcast(StateClosed)
		return
	}

	if err := proc.Start(); err != nil {
		log.Println("start: process:", err)
		n.broadcast(StateClosed)
		return
	}

	if err := waitReady(&n.cfg); err != nil {
		if err2 := proc.Stop(); err2 != nil {
			log.Println("start: shutdown process that did not start in time:", err2)
		}
		log.Println("start: process did not start:", err)
		n.broadcast(StateClosed)
		return
	}

	n.m.Lock()
	n.res = proc
	n.m.Unlock()

	go n.monitor(proc.PID())

	n.broadcast(StateOpen)
}

func waitReady(cfg *ProcessPoolConfig) error {
	i := 0

	for start := time.Now(); time.Since(start) < cfg.StartTimeout; i++ {
		time.Sleep(cfg.ReadyHealthCheckInterval)

		conn, err := net.DialTCP("tcp", nil, cfg.Dest)
		if err == nil {
			_ = conn.Close()
			if i > 0 {
				log.Println("start: process successfully started after",i,"retries")
			}
			return nil
		}

		log.Println("start: process not ready:", err, "waiting.")
	}

	return context.DeadlineExceeded
}

// monitor monitors resource activity and shutdowns it if it is idle or exit
func (n *pool) monitor(pid int) {
	log.Printf("monitor: monitoring [%d]", pid)

	child := make(chan os.Signal, 1)
	signal.Notify(child, syscall.SIGCHLD)
	defer signal.Stop(child)

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

iter:
	for {
		n.m.Lock()

		// assert
		if n.res.PID() != pid {
			log.Printf("shutdown: observed unknown pid. expected %d, got %d. exit.", pid, n.res.PID())
			n.m.Unlock()
			return
		}

		if n.open == 0 && time.Since(n.last) >= n.cfg.IdleTimeout {
			// assert
			if n.state != StateOpen {
				log.Fatalln("monitor: unexpected state before shutdown:", n.state)
			}

			n.state = StateShutdown
			n.m.Unlock()
			break
		}

		n.m.Unlock()

		select {
		// timer
		case <- ticker.C:
		// child process signals
		case <- child:
			var status syscall.WaitStatus
			_, err := syscall.Wait4(pid, &status, syscall.WNOHANG, nil)
			log.Println("monitor: got signal from [", pid, "]:", status)
			if err != nil {
				log.Println("monitor: wait4 error:", err)
				continue
			}

			// shutdown
			n.m.Lock()

			// assert
			if n.res.PID() != pid {
				log.Printf("shutdown: observed unknown pid. expected %d, got %d. exit.", pid, n.res.PID())
				n.m.Unlock()
				return
			}

			// assert
			if n.state != StateOpen {
				log.Fatalln("monitor: unexpected state before shutdown:", n.state)
			}

			n.state = StateShutdown
			n.m.Unlock()

			break iter
		}
	}

	// shutdown resource
	log.Printf("monitor: shutdown [%d]", pid)

	n.shutdown(pid)

	log.Printf("monitor: finished [%d]", pid)
}

func (n *pool) shutdown(pid int) {
	n.m.Lock()
	var cmd = n.res
	// assert
	if cmd.PID() != pid {
		log.Fatalf("shutdown: observed unknown pid. expected %d, got %d. exit.", pid, cmd.PID())
	}
	// assert
	if n.state != StateShutdown {
		log.Fatalln("shutdown: unexpected state:", n.state)
	}
	n.m.Unlock()

	// it must be safe to Stop() process not under the lock here because
	// shutdown() assumes that the coroutine exclusively owns the resource control
	// as far as the caller has successfully managed to switch the pool state.
	// so that any operation on the resource must be waiting for the shutdown
	// to complete and thus is ordered.
	if err := cmd.Stop(); err != nil {
		// I am relaxing error handling here allowing resources to leak.
		log.Println("shutdown: stop process:", err)
	}

	n.broadcast(StateClosed)

	log.Println("shutdown: closed")
}

// stats prints pool metrics regularly
func (n *pool) stats() {
	log.Print("stats: started")

	for {
		n.m.Lock()
		log.Println("stats:",
			"opened =",n.open,
			", last =",n.last.Format(time.RFC3339),
			", state =", stateString(n.state),
			", goroutines =", runtime.NumGoroutine())
		n.m.Unlock()

		time.Sleep(15*time.Second)
	}
}

func stateString(s int) string {
	switch s {
	case StateOpen: return "opened"
	case StateStarting: return "starting"
	case StateClosed: return "closed"
	case StateShutdown: return "shutdown"
	default: return "unknown"
	}
}
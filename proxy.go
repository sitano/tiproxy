package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"strings"
)

type Proxy struct {
	p Pool
}

func NewProxy(p Pool) *Proxy {
	return &Proxy{p: p}
}

func (p *Proxy) Do(bind string) error {
	addr, err := net.ResolveTCPAddr("tcp", bind)
	if err != nil {
		return fmt.Errorf("proxy: resolve bind: %w", err)
	}

	listener, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return fmt.Errorf("proxy: listen: %w", err)
	}

	log.Println("proxy: listening on", listener.Addr())

	for {
		src, err := listener.AcceptTCP()
		if err != nil {
			log.Println("proxy: accept connection failure:", err)
			continue
		}

		log.Println("proxy: new connection [", src.RemoteAddr(), "]")

		go p.handle(src)
	}
}

// handle manages new incoming connection
func (p *Proxy) handle(src *net.TCPConn) {
	defer src.Close()

	// pool.Get may block
	dst, err := p.p.Get()
	if err != nil {
		log.Println("destination acquisition:", err)
		return
	}

	// return resource back to the pool
	defer p.p.Put(dst)

	// backward copy
	go bridge(src, dst)

	// forward copy
	bridge(dst, src)

	log.Println("client disconnected [",src.RemoteAddr(),"]")
}

// bridge transfers data from src to dst
func bridge(dst, src net.Conn) {
	defer src.Close()
	// as there are usually 2 bridges per link, a bridge instance
	// must signal the second one about exit.
	defer dst.Close()

	for {
		moved, err := io.Copy(dst, src)
		if err != nil {
			if !isConnClosed(err) {
				log.Printf("bridge: %v", err)
			}
			break
		}

		// io.Copy never returns an io.EOF
		if moved == 0 {
			break
		}
	}
}

// isConnClosed checks if the error is the use of the closed connection.
//
// Historically Go authors have not exported the error that they
// return for an operation on a closed network connection.
// See: /usr/lib/go/src/internal/poll/fd.go: ErrNetClosing.
// err: *net.OpError
func isConnClosed(err error) bool {
	return strings.Contains(err.Error(), "use of closed network connection")
}

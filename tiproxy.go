package main

import (
	"context"
	"flag"
	"log"
	"net"
	"os"
	"time"
)

var bind = flag.String("bind", "0.0.0.0:4000", "address to bind to (--bind 127.0.0.1:4000)")
var dest = flag.String("dest", "", "destination resources address (--dest 127.0.0.1:3000)")
var idle = flag.Duration("idle", 30*time.Second, "idle resource timeout. the time before shutting down")
var start = flag.Duration("start", 30*time.Second, "resource ready status wait timeout on startup")
var ready = flag.Duration("ready", 500*time.Millisecond, "ready state health check interval on startup")
var workdir = flag.String("workdir", "", "resource work directory")
var execPath = flag.String("exec", "", "executable path to use as a resource")

// main executes proxy server. I am sorry it does not cleanup and handle signals.
//
// test: $ go build -race && ./tiproxy --dest 127.0.0.1:3000 --exec ncat --workdir /usr/sbin -- -l -k -p 3000 -4 -v
// test: $ curl -v http://127.0.0.1:4000/api
func main() {
	p, err := NewServicePool(config())
	if err != nil {
		log.Fatalln("service pool init:", err)
	}

	if err := NewProxy(p).Do(*bind); err != nil {
		log.Fatalln("proxy:", err)
	}
}

// config parses a command line of the form: executable --flags -- resource_flags
func config() ProcessPoolConfig {
	var args = findProcArgs()

	if err := flag.CommandLine.Parse(os.Args[1:len(os.Args) - len(args)]); err != nil {
		log.Fatalln("parse flags:", err)
	}

	if *bind == "" {
		log.Fatalln("--bind is mandatory")
	}

	if *execPath == "" {
		log.Fatalln("--exec is mandatory")
	}

	dest, err := net.ResolveTCPAddr("tcp", *dest)
	if err != nil {
		log.Fatalln("resolve tcp address:", err)
	}

	return ProcessPoolConfig{
		Ctx:          context.Background(),
		WorkDir:      *workdir,
		Exec:         *execPath,
		Args:         args,
		Dest:         dest,
		IdleTimeout:  *idle,
		StartTimeout: *start,
		ReadyHealthCheckInterval: *ready,
	}
}

func findProcArgs() []string {
	for i := 1; i<len(os.Args); i++ {
		if os.Args[i] == "--" {
			return os.Args[i+1:]
		}
	}
	return nil
}
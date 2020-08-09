package main

import (
	"context"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Process interface {
	Start() error
	Stop() error
	PID() int
	Wait() (*os.ProcessState, error)
}

type process struct {
	cmd *exec.Cmd
	pid int
	stop context.CancelFunc
}

var _ Process = (*process)(nil)

// NewProcess create a Process instance.
func NewProcess(ctx context.Context, dir, bin string, arg ...string) (Process, error) {
	ctx, stop := context.WithCancel(ctx)
	cmd := exec.CommandContext(ctx, bin, arg...)
	cmd.Dir = dir

	return &process{cmd: cmd, stop: stop}, nil
}

// Start the process
func (p *process) Start() error {
	log.Printf("starting `%s`: %s", filepath.Base(p.cmd.Path), strings.Join(p.cmd.Args, " "))
	if err := p.cmd.Start(); err != nil {
		return err
	}
	p.pid = p.cmd.Process.Pid
	log.Printf("process started [%d]", p.cmd.Process.Pid)
	return nil
}

// Stop the process
func (p *process) Stop() error {
	log.Printf("stopping process [%d]", p.cmd.Process.Pid)

	// This must be done to prevent leaking exec.Cmd coroutines
	// from /usr/lib/go/src/os/exec/exec.go:450. wait4 is not
	// reentrant, so when we catch sigchld and call wait4, exec.Cmd
	// fails to gracefully handle signal change.
	defer p.stop()

	// SIGKILL is unsafe but let's imagine children are stateless.
	err := p.cmd.Process.Kill()
	if err != nil {
		log.Println("send kill signal to the child process:",err)
		return err
	}

	// put process state in place
	if err := p.cmd.Wait(); err != nil && p.cmd.ProcessState == nil {
		log.Println("wait child process:", err)
		return err
	}

	log.Printf("process stopped [%d]: %s", p.cmd.Process.Pid, p.cmd.ProcessState.String())

	return nil
}

// PID returns process ID
func (p *process) PID() int {
	return p.pid
}

// Wait waits for the command to exit and waits for any copying to
// stdin or copying from stdout or stderr to complete.
func (p *process) Wait() (*os.ProcessState, error) {
	err := p.cmd.Wait()
	return p.cmd.ProcessState, err
}
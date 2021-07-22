package e2e

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"
)

// LongRunCmd defines a cmd which run for a long time
type LongRunCmd struct {
	cancel    context.CancelFunc
	cmd       *exec.Cmd
	stderrBuf bytes.Buffer
	stdoutBuf bytes.Buffer
	err       error
	done      bool
}

func NewLongRunCmd(dir string, args []string) *LongRunCmd {
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = dir

	var stderrBuf bytes.Buffer
	var stdoutBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	return &LongRunCmd{
		cmd:       cmd,
		cancel:    cancel,
		stderrBuf: stderrBuf,
		stdoutBuf: stdoutBuf,
	}
}

// Run in backend
func (l *LongRunCmd) Run() {
	go func() {
		err := l.cmd.Start()
		if err != nil {
			l.err = err
		}

		err = l.cmd.Wait()
		if err != nil {
			l.err = err
		}
		l.err = nil
		l.done = true
	}()
	time.Sleep(time.Millisecond * 100)
}

func (l *LongRunCmd) Stop() {
	l.cancel()
}

func (l *LongRunCmd) CheckExitStatus() error {
	if l.done == true {
		return l.err
	} else {
		return fmt.Errorf("running")
	}
}

func (l *LongRunCmd) GetStdoutStdErr() (string, string) {
	return l.stdoutBuf.String(), l.stderrBuf.String()
}

// RunShortRunCmd defines a cmd which run and exits immediately
func RunShortRunCmd(dir string, args []string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*20)
	defer cancel()

	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = dir

	output, err := cmd.CombinedOutput()
	return string(output), err
}
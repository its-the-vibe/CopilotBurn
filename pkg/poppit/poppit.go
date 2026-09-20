package poppit

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"sync"
)

// Command represents an executable command task.
type Command struct {
	ID     string
	Name   string
	Args   []string
	Target string
}

// CommandResult holds the execution result of a Command.
type CommandResult struct {
	Command Command
	Stdout  []byte
	Stderr  []byte
	Err     error
}

// Executor defines an interface for executing commands.
type Executor interface {
	Execute(ctx context.Context, cmd Command) CommandResult
}

// OSExecutor executes commands on the underlying operating system.
type OSExecutor struct{}

// NewOSExecutor creates a new OSExecutor.
func NewOSExecutor() *OSExecutor {
	return &OSExecutor{}
}

// Execute runs the command using exec.CommandContext.
func (e *OSExecutor) Execute(ctx context.Context, cmd Command) CommandResult {
	execCmd := exec.CommandContext(ctx, cmd.Name, cmd.Args...)
	var stdoutBuf, stderrBuf bytes.Buffer
	execCmd.Stdout = &stdoutBuf
	execCmd.Stderr = &stderrBuf

	err := execCmd.Run()
	return CommandResult{
		Command: cmd,
		Stdout:  stdoutBuf.Bytes(),
		Stderr:  stderrBuf.Bytes(),
		Err:     err,
	}
}

// Engine manages asynchronous command execution through a worker pool.
type Engine struct {
	executor Executor
	jobsChan chan Command
	results  chan CommandResult
	workers  int
	wg       sync.WaitGroup
	ctx      context.Context
	cancel   context.CancelFunc
}

// NewEngine initializes a Poppit engine with a specified executor, worker count, and buffer size.
func NewEngine(executor Executor, workers int, bufferSize int) *Engine {
	if executor == nil {
		executor = NewOSExecutor()
	}
	if workers <= 0 {
		workers = 4
	}
	if bufferSize <= 0 {
		bufferSize = 100
	}

	return &Engine{
		executor: executor,
		jobsChan: make(chan Command, bufferSize),
		results:  make(chan CommandResult, bufferSize),
		workers:  workers,
	}
}

// Start launches the worker pool using the provided context.
func (e *Engine) Start(ctx context.Context) {
	e.ctx, e.cancel = context.WithCancel(ctx)
	for i := 0; i < e.workers; i++ {
		e.wg.Add(1)
		go e.worker()
	}
}

func (e *Engine) worker() {
	defer e.wg.Done()
	for {
		select {
		case <-e.ctx.Done():
			return
		case cmd, ok := <-e.jobsChan:
			if !ok {
				return
			}
			res := e.executor.Execute(e.ctx, cmd)
			select {
			case <-e.ctx.Done():
				return
			case e.results <- res:
			}
		}
	}
}

// SubmitBatch submits a batch of commands for asynchronous execution.
func (e *Engine) SubmitBatch(cmds []Command) {
	for _, cmd := range cmds {
		select {
		case <-e.ctx.Done():
			return
		case e.jobsChan <- cmd:
		}
	}
}

// Submit command adds a single command to the queue.
func (e *Engine) Submit(cmd Command) {
	select {
	case <-e.ctx.Done():
		return
	case e.jobsChan <- cmd:
	}
}

// Results returns a read-only channel for command execution results.
func (e *Engine) Results() <-chan CommandResult {
	return e.results
}

// Stop safely stops workers after draining pending jobs and closes channels.
func (e *Engine) Stop() {
	close(e.jobsChan)
	e.wg.Wait()
	if e.cancel != nil {
		e.cancel()
	}
	close(e.results)
}

// String provides a string representation of Command.
func (c Command) String() string {
	return fmt.Sprintf("%s %v", c.Name, c.Args)
}

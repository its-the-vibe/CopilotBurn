package poppit_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/its-the-vibe/CopilotBurn/pkg/poppit"
)

type MockExecutor struct {
	Responses map[string]poppit.CommandResult
}

func (m *MockExecutor) Execute(ctx context.Context, cmd poppit.Command) poppit.CommandResult {
	if res, ok := m.Responses[cmd.ID]; ok {
		res.Command = cmd
		return res
	}
	return poppit.CommandResult{
		Command: cmd,
		Stdout:  []byte(fmt.Sprintf("output for %s", cmd.ID)),
	}
}

func TestEngine_SubmitAndResults(t *testing.T) {
	mockExec := &MockExecutor{}
	engine := poppit.NewEngine(mockExec, 2, 10)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	engine.Start(ctx)

	cmds := []poppit.Command{
		{ID: "cmd-1", Name: "gh", Args: []string{"vibe", "usage", "--day", "1"}},
		{ID: "cmd-2", Name: "gh", Args: []string{"vibe", "usage", "--day", "2"}},
		{ID: "cmd-3", Name: "gh", Args: []string{"vibe", "usage", "--day", "3"}},
	}

	engine.SubmitBatch(cmds)

	received := make(map[string]bool)
	for i := 0; i < len(cmds); i++ {
		select {
		case res := <-engine.Results():
			received[res.Command.ID] = true
		case <-ctx.Done():
			t.Fatal("timed out waiting for command results")
		}
	}

	engine.Stop()

	if len(received) != len(cmds) {
		t.Errorf("expected %d results, got %d", len(cmds), len(received))
	}
	for _, cmd := range cmds {
		if !received[cmd.ID] {
			t.Errorf("expected result for command %s, but did not receive it", cmd.ID)
		}
	}
}

func TestOSExecutor(t *testing.T) {
	exec := poppit.NewOSExecutor()
	cmd := poppit.Command{
		ID:   "test-echo",
		Name: "echo",
		Args: []string{"hello"},
	}

	ctx := context.Background()
	res := exec.Execute(ctx, cmd)

	if res.Err != nil {
		t.Fatalf("unexpected error executing echo: %v", res.Err)
	}
	if string(res.Stdout) != "hello\n" && string(res.Stdout) != "hello\r\n" {
		t.Errorf("expected stdout 'hello\\n', got %q", string(res.Stdout))
	}
}

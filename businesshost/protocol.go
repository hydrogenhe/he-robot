package businesshost

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
)

type rpcMessage struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}
type connection struct {
	command  *exec.Cmd
	input    io.WriteCloser
	messages chan rpcMessage
	errors   chan error
}

func start(ctx context.Context, path, work string, args []string, environment ...string) (*connection, error) {
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = work
	cmd.Env = businessEnvironment()
	cmd.Env = append(cmd.Env, environment...)
	input, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	c := &connection{command: cmd, input: input, messages: make(chan rpcMessage, 64), errors: make(chan error, 1)}
	go readMessages(ctx, output, c.messages, c.errors)
	return c, nil
}
func readMessages(ctx context.Context, reader io.Reader, messages chan<- rpcMessage, failures chan<- error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 4<<20)
	for scanner.Scan() {
		var m rpcMessage
		if json.Unmarshal(scanner.Bytes(), &m) != nil {
			continue
		}
		select {
		case messages <- m:
		case <-ctx.Done():
			return
		}
	}
	err := scanner.Err()
	if err == nil {
		err = io.EOF
	}
	failures <- err
}
func (c *connection) send(id int, method string, params any) error {
	value := map[string]any{"method": method, "params": params}
	if id != 0 {
		value["id"] = id
	}
	return json.NewEncoder(c.input).Encode(value)
}
func (c *connection) next(ctx context.Context) (rpcMessage, error) {
	select {
	case value := <-c.messages:
		return value, nil
	case err := <-c.errors:
		return rpcMessage{}, err
	case <-ctx.Done():
		return rpcMessage{}, ctx.Err()
	}
}
func (c *connection) wait(ctx context.Context, id int) (json.RawMessage, error) {
	for {
		m, err := c.next(ctx)
		if err != nil {
			return nil, err
		}
		if string(m.ID) != fmt.Sprint(id) || m.Method != "" {
			continue
		}
		if m.Error != nil {
			return nil, fmt.Errorf("Codex %d: %s", m.Error.Code, m.Error.Message)
		}
		return m.Result, nil
	}
}
func (c *connection) close() { c.input.Close(); c.command.Process.Kill(); c.command.Wait() }

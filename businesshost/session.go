package businesshost

import (
	"context"
	"os"
)

func openSession(ctx context.Context, root string, generation bool) (*connection, string, map[string]any, func(), error) {
	path, err := codexExecutable()
	if err != nil {
		return nil, "", nil, nil, err
	}
	work, err := os.MkdirTemp("", "gpt-robot-business-")
	if err != nil {
		return nil, "", nil, nil, err
	}
	args := profileArgs(root)
	if generation {
		args = append(args, "-c", "mcp_servers.gpt_robot.enabled=false", "-c", "features.code_mode_host=false")
	}
	c, err := start(ctx, path, work, args)
	if err != nil {
		os.RemoveAll(work)
		return nil, "", nil, nil, err
	}
	cleanup := (&sessionCleanup{c: c, work: work}).close
	init := map[string]any{"clientInfo": map[string]any{"name": "gpt_robot_business", "version": "0.2.0"}, "capabilities": map[string]any{"experimentalApi": true}}
	if err = c.send(1, "initialize", init); err == nil {
		_, err = c.wait(ctx, 1)
	}
	if err == nil {
		err = c.send(0, "initialized", map[string]any{})
	}
	if err == nil {
		err = c.send(2, "skills/list", map[string]any{"cwds": []string{work}})
	}
	if err != nil {
		cleanup()
		return nil, "", nil, nil, err
	}
	raw, err := c.wait(ctx, 2)
	if err != nil {
		cleanup()
		return nil, "", nil, nil, err
	}
	overrides, err := businessOverrides(raw)
	if err != nil {
		cleanup()
		return nil, "", nil, nil, err
	}
	if generation {
		if err := generationOverrides(ctx, c, work, overrides); err != nil {
			cleanup()
			return nil, "", nil, nil, err
		}
	}
	return c, work, overrides, cleanup, nil
}

type sessionCleanup struct {
	c    *connection
	work string
}

func (s *sessionCleanup) close() { s.c.close(); os.RemoveAll(s.work) }

package plugin

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/mitchellh/mapstructure"
	"github.com/rs/zerolog"
)

type ShellConfig struct {
	Command string   `mapstructure:"command"`
	Args    []string `mapstructure:"args"`
}

type ShellPlugin struct {
	command string
	args    []string
}

func NewShellPlugin(config PluginConfig) (Store, error) {
	var cfg ShellConfig
	if err := mapstructure.Decode(config, &cfg); err != nil {
		return nil, fmt.Errorf("failed to decode shell config: %w", err)
	}

	if cfg.Command == "" {
		return nil, fmt.Errorf("command is required for shell plugin")
	}

	return &ShellPlugin{
		command: cfg.Command,
		args:    cfg.Args,
	}, nil
}

func (p *ShellPlugin) Get(ctx context.Context, key string) (string, error) {
	logger := zerolog.Ctx(ctx)
	logger.Info().Str("key", key).Msg("get data from shell plugin")

	stdout, stderr, err := p.executeCommand(ctx, OpGet, key, "")
	if err != nil {
		if stderr != "" {
			return "", fmt.Errorf("%w (stderr: %s)", err, stderr)
		}
		return "", err
	}

	// Log stderr if present (for debugging)
	if stderr != "" {
		logger.Debug().Str("stderr", stderr).Msg("shell plugin stderr")
	}

	return stdout, nil
}

func (p *ShellPlugin) Set(ctx context.Context, key string, value string) error {
	logger := zerolog.Ctx(ctx)
	logger.Info().Str("key", key).Msg("set data to shell plugin")

	_, stderr, err := p.executeCommand(ctx, OpSet, key, value)
	if err != nil {
		if stderr != "" {
			return fmt.Errorf("%w (stderr: %s)", err, stderr)
		}
		return err
	}

	// Log stderr if present (for debugging)
	if stderr != "" {
		logger.Debug().Str("stderr", stderr).Msg("shell plugin stderr")
	}

	return nil
}

func (p *ShellPlugin) executeCommand(ctx context.Context, action, key, value string) (string, string, error) {
	cmd := exec.CommandContext(ctx, p.command, p.args...)

	// Prepare stdin with shell variable assignments
	var stdinBuf bytes.Buffer
	stdinBuf.WriteString(fmt.Sprintf("STUNMESH_ACTION=%s\n", action))
	stdinBuf.WriteString(fmt.Sprintf("STUNMESH_KEY=%s\n", key))
	if action == OpSet {
		stdinBuf.WriteString(fmt.Sprintf("STUNMESH_VALUE=%s\n", value))
	}

	cmd.Stdin = &stdinBuf

	// Capture both stdout and stderr
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	// Execute command
	if err := cmd.Run(); err != nil {
		return "", strings.TrimSpace(stderrBuf.String()), fmt.Errorf("command execution failed: %w", err)
	}

	return strings.TrimSpace(stdoutBuf.String()), strings.TrimSpace(stderrBuf.String()), nil
}

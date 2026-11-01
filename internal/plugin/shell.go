package plugin

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/go-viper/mapstructure/v2"
	"github.com/rs/zerolog"
	pluginapi "github.com/tjjh89017/stunmesh-go/pluginapi"
)

type ShellConfig struct {
	Command string   `mapstructure:"command"`
	Args    []string `mapstructure:"args"`
}

type ShellPlugin struct {
	command string
	args    []string
}

func NewShellPlugin(config pluginapi.PluginConfig) (pluginapi.Store, error) {
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

func (p *ShellPlugin) Get(ctx context.Context, req pluginapi.PluginRequest) (string, error) {
	logger := zerolog.Ctx(ctx)
	logger.Info().Str("key", req.Key).Msg("get data from shell plugin")

	stdout, stderr, err := p.executeCommand(ctx, pluginapi.OpGet, req)
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

func (p *ShellPlugin) Set(ctx context.Context, req pluginapi.PluginRequest) error {
	logger := zerolog.Ctx(ctx)
	logger.Info().Str("key", req.Key).Msg("set data to shell plugin")

	_, stderr, err := p.executeCommand(ctx, pluginapi.OpSet, req)
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

func (p *ShellPlugin) executeCommand(ctx context.Context, action string, req pluginapi.PluginRequest) (string, string, error) {
	cmd := exec.CommandContext(ctx, p.command, p.args...)

	// Prepare stdin with shell variable assignments
	var stdinBuf bytes.Buffer
	fmt.Fprintf(&stdinBuf, "STUNMESH_ACTION=%s\n", action)
	fmt.Fprintf(&stdinBuf, "STUNMESH_KEY=%s\n", req.Key)
	if action == pluginapi.OpSet {
		fmt.Fprintf(&stdinBuf, "STUNMESH_VALUE=%s\n", req.Value)
	}
	fmt.Fprintf(&stdinBuf, "STUNMESH_DEVICE=%s\n", shellQuote(req.Device))
	fmt.Fprintf(&stdinBuf, "STUNMESH_LOCAL_PUBLIC_KEY=%s\n", keyString(req.LocalPublicKey))
	fmt.Fprintf(&stdinBuf, "STUNMESH_PEER_PUBLIC_KEY=%s\n", keyString(req.PeerPublicKey))

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

// shellQuote single-quotes s for POSIX sh and drops control characters, so
// the value stays on one line for readers that parse line by line.
func shellQuote(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

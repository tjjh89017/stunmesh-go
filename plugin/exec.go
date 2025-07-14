package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"

	"github.com/mitchellh/mapstructure"
	"github.com/rs/zerolog"
)

type ExecConfig struct {
	Command string   `mapstructure:"command"`
	Args    []string `mapstructure:"args"`
}

type ExecPlugin struct {
	command string
	args    []string
}

type ExecRequest struct {
	Action string `json:"action"`
	Key    string `json:"key"`
	Value  string `json:"value,omitempty"`
}

type ExecResponse struct {
	Success bool   `json:"success"`
	Value   string `json:"value,omitempty"`
	Error   string `json:"error,omitempty"`
}

func NewExecPlugin(config PluginConfig) (Store, error) {
	var cfg ExecConfig
	if err := mapstructure.Decode(config, &cfg); err != nil {
		return nil, fmt.Errorf("failed to decode exec config: %w", err)
	}

	if cfg.Command == "" {
		return nil, fmt.Errorf("command is required for exec plugin")
	}

	return &ExecPlugin{
		command: cfg.Command,
		args:    cfg.Args,
	}, nil
}

func (p *ExecPlugin) Get(ctx context.Context, key string) (string, error) {
	logger := zerolog.Ctx(ctx)
	logger.Info().Str("key", key).Msg("get data from exec plugin")

	request := ExecRequest{
		Action: "get",
		Key:    key,
	}

	response, err := p.executeCommand(ctx, request)
	if err != nil {
		return "", err
	}

	if !response.Success {
		return "", fmt.Errorf("exec plugin error: %s", response.Error)
	}

	return response.Value, nil
}

func (p *ExecPlugin) Set(ctx context.Context, key string, value string) error {
	logger := zerolog.Ctx(ctx)
	logger.Info().Str("key", key).Str("value", value).Msg("set data to exec plugin")

	request := ExecRequest{
		Action: "set",
		Key:    key,
		Value:  value,
	}

	response, err := p.executeCommand(ctx, request)
	if err != nil {
		return err
	}

	if !response.Success {
		return fmt.Errorf("exec plugin error: %s", response.Error)
	}

	return nil
}

func (p *ExecPlugin) executeCommand(ctx context.Context, request ExecRequest) (*ExecResponse, error) {
	cmd := exec.CommandContext(ctx, p.command, p.args...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start command: %w", err)
	}

	// Send request to stdin
	encoder := json.NewEncoder(stdin)
	if err := encoder.Encode(request); err != nil {
		stdin.Close()
		return nil, fmt.Errorf("failed to encode request: %w", err)
	}
	stdin.Close()

	// Read response from stdout
	var response ExecResponse
	decoder := json.NewDecoder(stdout)
	if err := decoder.Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if err := cmd.Wait(); err != nil {
		return nil, fmt.Errorf("command execution failed: %w", err)
	}

	return &response, nil
}

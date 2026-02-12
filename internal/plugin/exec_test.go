package plugin

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	pluginapi "github.com/tjjh89017/stunmesh-go/pluginapi"
)

func getTestPluginPath(filename string) string {
	return filepath.Join("testdata", filename)
}

func TestNewExecPlugin_Success(t *testing.T) {
	config := pluginapi.PluginConfig{
		"command": "/bin/echo",
		"args":    []string{"test"},
	}

	plugin, err := NewExecPlugin(config)
	if err != nil {
		t.Fatalf("NewExecPlugin() error = %v, want nil", err)
	}

	if plugin == nil {
		t.Fatal("NewExecPlugin() returned nil plugin")
	}

	execPlugin, ok := plugin.(*ExecPlugin)
	if !ok {
		t.Fatalf("NewExecPlugin() returned wrong type: %T", plugin)
	}

	if execPlugin.command != "/bin/echo" {
		t.Errorf("ExecPlugin.command = %q, want %q", execPlugin.command, "/bin/echo")
	}

	if len(execPlugin.args) != 1 || execPlugin.args[0] != "test" {
		t.Errorf("ExecPlugin.args = %v, want [\"test\"]", execPlugin.args)
	}
}

func TestNewExecPlugin_MissingCommand(t *testing.T) {
	config := pluginapi.PluginConfig{
		"args": []string{"test"},
		// Missing "command" field
	}

	plugin, err := NewExecPlugin(config)
	if err == nil {
		t.Error("NewExecPlugin() should return error when command is missing")
	}

	if plugin != nil {
		t.Error("NewExecPlugin() should return nil plugin on error")
	}

	expectedMsg := "command is required for exec plugin"
	if err != nil && err.Error() != expectedMsg {
		t.Errorf("NewExecPlugin() error = %q, want %q", err.Error(), expectedMsg)
	}
}

func TestNewExecPlugin_EmptyCommand(t *testing.T) {
	config := pluginapi.PluginConfig{
		"command": "",
		"args":    []string{"test"},
	}

	plugin, err := NewExecPlugin(config)
	if err == nil {
		t.Error("NewExecPlugin() should return error when command is empty")
	}

	if plugin != nil {
		t.Error("NewExecPlugin() should return nil plugin on error")
	}
}

func TestNewExecPlugin_NoArgs(t *testing.T) {
	config := pluginapi.PluginConfig{
		"command": "/bin/echo",
		// No args is valid
	}

	plugin, err := NewExecPlugin(config)
	if err != nil {
		t.Errorf("NewExecPlugin() without args should succeed, got error: %v", err)
	}

	if plugin == nil {
		t.Fatal("NewExecPlugin() returned nil plugin")
	}

	execPlugin := plugin.(*ExecPlugin)
	if len(execPlugin.args) != 0 {
		t.Errorf("ExecPlugin.args = %v, want empty slice", execPlugin.args)
	}
}

func TestExecPlugin_Get_Success(t *testing.T) {
	// Skip if test plugin doesn't exist
	pluginPath := getTestPluginPath("exec_test_plugin.sh")
	if _, err := os.Stat(pluginPath); os.IsNotExist(err) {
		t.Skip("Test plugin not found:", pluginPath)
	}

	config := pluginapi.PluginConfig{
		"command": pluginPath,
	}

	plugin, err := NewExecPlugin(config)
	if err != nil {
		t.Fatalf("NewExecPlugin() error = %v", err)
	}

	ctx := context.Background()

	// First set a value
	testKey := "testkey_get"
	testValue := "testvalue"

	err = plugin.Set(ctx, testKey, testValue)
	if err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	// Then get it back
	gotValue, err := plugin.Get(ctx, testKey)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if gotValue != testValue {
		t.Errorf("Get() = %q, want %q", gotValue, testValue)
	}

	// Cleanup
	storageDir := "/tmp/stunmesh-test-plugin"
	_ = os.RemoveAll(storageDir)
}

func TestExecPlugin_Get_NotFound(t *testing.T) {
	// Skip if test plugin doesn't exist
	pluginPath := getTestPluginPath("exec_test_plugin.sh")
	if _, err := os.Stat(pluginPath); os.IsNotExist(err) {
		t.Skip("Test plugin not found:", pluginPath)
	}

	// Cleanup before test
	storageDir := "/tmp/stunmesh-test-plugin"
	_ = os.RemoveAll(storageDir)

	config := pluginapi.PluginConfig{
		"command": pluginPath,
	}

	plugin, err := NewExecPlugin(config)
	if err != nil {
		t.Fatalf("NewExecPlugin() error = %v", err)
	}

	ctx := context.Background()

	// Try to get a nonexistent key
	_, err = plugin.Get(ctx, "nonexistent_key")
	if err == nil {
		t.Error("Get() should return error for nonexistent key")
	}

	// Cleanup
	_ = os.RemoveAll(storageDir)
}

func TestExecPlugin_Set_Success(t *testing.T) {
	// Skip if test plugin doesn't exist
	pluginPath := getTestPluginPath("exec_test_plugin.sh")
	if _, err := os.Stat(pluginPath); os.IsNotExist(err) {
		t.Skip("Test plugin not found:", pluginPath)
	}

	config := pluginapi.PluginConfig{
		"command": pluginPath,
	}

	plugin, err := NewExecPlugin(config)
	if err != nil {
		t.Fatalf("NewExecPlugin() error = %v", err)
	}

	ctx := context.Background()

	testKey := "testkey_set"
	testValue := "testvalue"

	err = plugin.Set(ctx, testKey, testValue)
	if err != nil {
		t.Errorf("Set() error = %v, want nil", err)
	}

	// Cleanup
	storageDir := "/tmp/stunmesh-test-plugin"
	_ = os.RemoveAll(storageDir)
}

func TestExecPlugin_CommandNotFound(t *testing.T) {
	config := pluginapi.PluginConfig{
		"command": "/nonexistent/command",
	}

	plugin, err := NewExecPlugin(config)
	if err != nil {
		t.Fatalf("NewExecPlugin() error = %v", err)
	}

	ctx := context.Background()

	// Try to execute - should fail because command doesn't exist
	_, err = plugin.Get(ctx, "testkey")
	if err == nil {
		t.Error("Get() should return error when command doesn't exist")
	}
}

func TestExecPlugin_InvalidJSON(t *testing.T) {
	// Skip if test plugin doesn't exist
	pluginPath := getTestPluginPath("exec_invalid_json.sh")
	if _, err := os.Stat(pluginPath); os.IsNotExist(err) {
		t.Skip("Test plugin not found:", pluginPath)
	}

	config := pluginapi.PluginConfig{
		"command": pluginPath,
	}

	plugin, err := NewExecPlugin(config)
	if err != nil {
		t.Fatalf("NewExecPlugin() error = %v", err)
	}

	ctx := context.Background()

	// Try to get - should fail due to invalid JSON response
	_, err = plugin.Get(ctx, "testkey")
	if err == nil {
		t.Error("Get() should return error when plugin returns invalid JSON")
	}
}

func TestExecPlugin_PluginFailure(t *testing.T) {
	// Skip if test plugin doesn't exist
	pluginPath := getTestPluginPath("exec_fail_plugin.sh")
	if _, err := os.Stat(pluginPath); os.IsNotExist(err) {
		t.Skip("Test plugin not found:", pluginPath)
	}

	config := pluginapi.PluginConfig{
		"command": pluginPath,
	}

	plugin, err := NewExecPlugin(config)
	if err != nil {
		t.Fatalf("NewExecPlugin() error = %v", err)
	}

	ctx := context.Background()

	// Try to get - should fail with plugin error
	_, err = plugin.Get(ctx, "testkey")
	if err == nil {
		t.Error("Get() should return error when plugin fails")
	}
}

func TestExecPlugin_SetFailure(t *testing.T) {
	// Skip if test plugin doesn't exist
	pluginPath := getTestPluginPath("exec_fail_plugin.sh")
	if _, err := os.Stat(pluginPath); os.IsNotExist(err) {
		t.Skip("Test plugin not found:", pluginPath)
	}

	config := pluginapi.PluginConfig{
		"command": pluginPath,
	}

	plugin, err := NewExecPlugin(config)
	if err != nil {
		t.Fatalf("NewExecPlugin() error = %v", err)
	}

	ctx := context.Background()

	// Try to set - should fail with plugin error
	err = plugin.Set(ctx, "testkey", "testvalue")
	if err == nil {
		t.Error("Set() should return error when plugin fails")
	}
}

func TestExecPlugin_WithArgs(t *testing.T) {
	config := pluginapi.PluginConfig{
		"command": "/bin/sh",
		"args":    []string{"-c", "cat > /dev/null && echo '{\"success\":true,\"value\":\"test\"}'"},
	}

	plugin, err := NewExecPlugin(config)
	if err != nil {
		t.Fatalf("NewExecPlugin() error = %v", err)
	}

	execPlugin := plugin.(*ExecPlugin)
	if len(execPlugin.args) != 2 {
		t.Errorf("ExecPlugin.args length = %d, want 2", len(execPlugin.args))
	}

	ctx := context.Background()

	// Should succeed and return "test"
	value, err := plugin.Get(ctx, "anykey")
	if err != nil {
		t.Errorf("Get() error = %v, want nil", err)
	}

	if value != "test" {
		t.Errorf("Get() = %q, want %q", value, "test")
	}
}

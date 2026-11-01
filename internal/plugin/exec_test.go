package plugin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	pluginapi "github.com/tjjh89017/stunmesh-go/pluginapi"
)

func getTestPluginPath(filename string) string {
	return filepath.Join("testdata", filename)
}

func skipOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("plugin exec tests use .sh fixtures")
	}
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
	skipOnWindows(t)
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

	// A fixture unaware of the identity fields must keep working.
	req := testRequest(testKey, "wg0")
	req.Value = testValue
	err = plugin.Set(ctx, req)
	if err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	// Then get it back
	gotValue, err := plugin.Get(ctx, req)
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
	skipOnWindows(t)
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
	_, err = plugin.Get(ctx, pluginapi.PluginRequest{Key: "nonexistent_key"})
	if err == nil {
		t.Error("Get() should return error for nonexistent key")
	}

	// Cleanup
	_ = os.RemoveAll(storageDir)
}

func TestExecPlugin_Set_Success(t *testing.T) {
	skipOnWindows(t)
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

	err = plugin.Set(ctx, pluginapi.PluginRequest{Key: testKey, Value: testValue})
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
	_, err = plugin.Get(ctx, pluginapi.PluginRequest{Key: "testkey"})
	if err == nil {
		t.Error("Get() should return error when command doesn't exist")
	}
}

func TestExecPlugin_InvalidJSON(t *testing.T) {
	skipOnWindows(t)
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
	_, err = plugin.Get(ctx, pluginapi.PluginRequest{Key: "testkey"})
	if err == nil {
		t.Error("Get() should return error when plugin returns invalid JSON")
	}
}

func TestExecPlugin_PluginFailure(t *testing.T) {
	skipOnWindows(t)
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
	_, err = plugin.Get(ctx, pluginapi.PluginRequest{Key: "testkey"})
	if err == nil {
		t.Error("Get() should return error when plugin fails")
	}
}

func TestExecPlugin_SetFailure(t *testing.T) {
	skipOnWindows(t)
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
	err = plugin.Set(ctx, pluginapi.PluginRequest{Key: "testkey", Value: "testvalue"})
	if err == nil {
		t.Error("Set() should return error when plugin fails")
	}
}

func TestExecPlugin_WithArgs(t *testing.T) {
	skipOnWindows(t)
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
	value, err := plugin.Get(ctx, pluginapi.PluginRequest{Key: "anykey"})
	if err != nil {
		t.Errorf("Get() error = %v, want nil", err)
	}

	if value != "test" {
		t.Errorf("Get() = %q, want %q", value, "test")
	}
}

func testRequest(key, device string) pluginapi.PluginRequest {
	var local, peer pluginapi.Key
	for i := range local {
		local[i] = byte(i + 1)
		peer[i] = byte(i + 101)
	}
	return pluginapi.PluginRequest{Device: device, LocalPublicKey: local, PeerPublicKey: peer, Key: key}
}

func TestExecPlugin_RequestCarriesIdentity(t *testing.T) {
	skipOnWindows(t)
	out := filepath.Join(t.TempDir(), "request.json")
	plugin, err := NewExecPlugin(pluginapi.PluginConfig{
		"command": "/bin/sh",
		"args":    []string{"-c", `cat > "$0"; echo '{"success":true,"value":"v"}'`, out},
	})
	if err != nil {
		t.Fatalf("NewExecPlugin() error = %v", err)
	}
	want := testRequest("k", "wg0")

	for _, action := range []string{pluginapi.OpGet, pluginapi.OpSet} {
		if action == pluginapi.OpGet {
			_, err = plugin.Get(context.Background(), want)
		} else {
			setReq := want
			setReq.Value = "v"
			err = plugin.Set(context.Background(), setReq)
		}
		if err != nil {
			t.Fatalf("%s error = %v", action, err)
		}
		raw, err := os.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}
		var req pluginapi.ExecRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			t.Fatalf("unmarshal %q: %v", raw, err)
		}
		if req.Action != action || req.Key != "k" || req.Device != "wg0" ||
			req.LocalPublicKey != want.LocalPublicKey.String() ||
			req.PeerPublicKey != want.PeerPublicKey.String() {
			t.Errorf("request = %+v, want %+v", req, want)
		}
	}
}

func TestExecPlugin_UnknownIdentityOmitted(t *testing.T) {
	skipOnWindows(t)
	out := filepath.Join(t.TempDir(), "request.json")
	plugin, err := NewExecPlugin(pluginapi.PluginConfig{
		"command": "/bin/sh",
		"args":    []string{"-c", `cat > "$0"; echo '{"success":true}'`, out},
	})
	if err != nil {
		t.Fatalf("NewExecPlugin() error = %v", err)
	}
	if err := plugin.Set(context.Background(), pluginapi.PluginRequest{Key: "k", Value: "v"}); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"device", "local_public_key", "peer_public_key"} {
		if _, ok := fields[k]; ok {
			t.Errorf("field %q present for unknown identity: %s", k, raw)
		}
	}
}

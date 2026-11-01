package plugin

import (
	"context"
	"os"
	"testing"

	pluginapi "github.com/tjjh89017/stunmesh-go/pluginapi"
)

func TestNewShellPlugin_Success(t *testing.T) {
	config := pluginapi.PluginConfig{
		"command": "/bin/sh",
		"args":    []string{"-c", "echo test"},
	}

	plugin, err := NewShellPlugin(config)
	if err != nil {
		t.Fatalf("NewShellPlugin() error = %v, want nil", err)
	}

	if plugin == nil {
		t.Fatal("NewShellPlugin() returned nil plugin")
	}

	shellPlugin, ok := plugin.(*ShellPlugin)
	if !ok {
		t.Fatalf("NewShellPlugin() returned wrong type: %T", plugin)
	}

	if shellPlugin.command != "/bin/sh" {
		t.Errorf("ShellPlugin.command = %q, want %q", shellPlugin.command, "/bin/sh")
	}

	if len(shellPlugin.args) != 2 {
		t.Errorf("ShellPlugin.args length = %d, want 2", len(shellPlugin.args))
	}
}

func TestNewShellPlugin_MissingCommand(t *testing.T) {
	config := pluginapi.PluginConfig{
		"args": []string{"test"},
		// Missing "command" field
	}

	plugin, err := NewShellPlugin(config)
	if err == nil {
		t.Error("NewShellPlugin() should return error when command is missing")
	}

	if plugin != nil {
		t.Error("NewShellPlugin() should return nil plugin on error")
	}

	expectedMsg := "command is required for shell plugin"
	if err != nil && err.Error() != expectedMsg {
		t.Errorf("NewShellPlugin() error = %q, want %q", err.Error(), expectedMsg)
	}
}

func TestNewShellPlugin_EmptyCommand(t *testing.T) {
	config := pluginapi.PluginConfig{
		"command": "",
		"args":    []string{"test"},
	}

	plugin, err := NewShellPlugin(config)
	if err == nil {
		t.Error("NewShellPlugin() should return error when command is empty")
	}

	if plugin != nil {
		t.Error("NewShellPlugin() should return nil plugin on error")
	}
}

func TestNewShellPlugin_NoArgs(t *testing.T) {
	config := pluginapi.PluginConfig{
		"command": "/bin/sh",
		// No args is valid
	}

	plugin, err := NewShellPlugin(config)
	if err != nil {
		t.Errorf("NewShellPlugin() without args should succeed, got error: %v", err)
	}

	if plugin == nil {
		t.Fatal("NewShellPlugin() returned nil plugin")
	}

	shellPlugin := plugin.(*ShellPlugin)
	if len(shellPlugin.args) != 0 {
		t.Errorf("ShellPlugin.args = %v, want empty slice", shellPlugin.args)
	}
}

func TestShellPlugin_Get_Success(t *testing.T) {
	skipOnWindows(t)
	// Skip if test plugin doesn't exist
	pluginPath := getTestPluginPath("shell_test_plugin.sh")
	if _, err := os.Stat(pluginPath); os.IsNotExist(err) {
		t.Skip("Test plugin not found:", pluginPath)
	}

	config := pluginapi.PluginConfig{
		"command": pluginPath,
	}

	plugin, err := NewShellPlugin(config)
	if err != nil {
		t.Fatalf("NewShellPlugin() error = %v", err)
	}

	ctx := context.Background()

	// First set a value
	testKey := "testkey_get"
	testValue := "testvalue"

	err = plugin.Set(ctx, pluginapi.PluginRequest{Key: testKey, Value: testValue})
	if err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	// Then get it back
	gotValue, err := plugin.Get(ctx, pluginapi.PluginRequest{Key: testKey})
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if gotValue != testValue {
		t.Errorf("Get() = %q, want %q", gotValue, testValue)
	}

	// Cleanup
	storageDir := "/tmp/stunmesh-test-shell-plugin"
	_ = os.RemoveAll(storageDir)
}

func TestShellPlugin_Get_NotFound(t *testing.T) {
	skipOnWindows(t)
	// Skip if test plugin doesn't exist
	pluginPath := getTestPluginPath("shell_test_plugin.sh")
	if _, err := os.Stat(pluginPath); os.IsNotExist(err) {
		t.Skip("Test plugin not found:", pluginPath)
	}

	// Cleanup before test
	storageDir := "/tmp/stunmesh-test-shell-plugin"
	_ = os.RemoveAll(storageDir)

	config := pluginapi.PluginConfig{
		"command": pluginPath,
	}

	plugin, err := NewShellPlugin(config)
	if err != nil {
		t.Fatalf("NewShellPlugin() error = %v", err)
	}

	ctx := context.Background()

	// Try to get a nonexistent key
	_, err = plugin.Get(ctx, pluginapi.PluginRequest{Key: "nonexistent_key"})
	if err == nil {
		t.Error("Get() should return error for nonexistent key")
	}

	// Error should include stderr message
	if err != nil && err.Error() == "" {
		t.Error("Get() error message should not be empty")
	}

	// Cleanup
	_ = os.RemoveAll(storageDir)
}

func TestShellPlugin_Set_Success(t *testing.T) {
	skipOnWindows(t)
	// Skip if test plugin doesn't exist
	pluginPath := getTestPluginPath("shell_test_plugin.sh")
	if _, err := os.Stat(pluginPath); os.IsNotExist(err) {
		t.Skip("Test plugin not found:", pluginPath)
	}

	config := pluginapi.PluginConfig{
		"command": pluginPath,
	}

	plugin, err := NewShellPlugin(config)
	if err != nil {
		t.Fatalf("NewShellPlugin() error = %v", err)
	}

	ctx := context.Background()

	testKey := "testkey_set"
	testValue := "testvalue"

	err = plugin.Set(ctx, pluginapi.PluginRequest{Key: testKey, Value: testValue})
	if err != nil {
		t.Errorf("Set() error = %v, want nil", err)
	}

	// Cleanup
	storageDir := "/tmp/stunmesh-test-shell-plugin"
	_ = os.RemoveAll(storageDir)
}

func TestShellPlugin_CommandNotFound(t *testing.T) {
	config := pluginapi.PluginConfig{
		"command": "/nonexistent/command",
	}

	plugin, err := NewShellPlugin(config)
	if err != nil {
		t.Fatalf("NewShellPlugin() error = %v", err)
	}

	ctx := context.Background()

	// Try to execute - should fail because command doesn't exist
	_, err = plugin.Get(ctx, pluginapi.PluginRequest{Key: "testkey"})
	if err == nil {
		t.Error("Get() should return error when command doesn't exist")
	}
}

func TestShellPlugin_PluginFailure(t *testing.T) {
	skipOnWindows(t)
	// Skip if test plugin doesn't exist
	pluginPath := getTestPluginPath("shell_fail_plugin.sh")
	if _, err := os.Stat(pluginPath); os.IsNotExist(err) {
		t.Skip("Test plugin not found:", pluginPath)
	}

	config := pluginapi.PluginConfig{
		"command": pluginPath,
	}

	plugin, err := NewShellPlugin(config)
	if err != nil {
		t.Fatalf("NewShellPlugin() error = %v", err)
	}

	ctx := context.Background()

	// Try to get - should fail with plugin error
	_, err = plugin.Get(ctx, pluginapi.PluginRequest{Key: "testkey"})
	if err == nil {
		t.Error("Get() should return error when plugin fails")
	}

	// Error should include stderr
	if err != nil {
		errMsg := err.Error()
		if errMsg == "" {
			t.Error("Get() error message should not be empty")
		}
	}
}

func TestShellPlugin_SetFailure(t *testing.T) {
	skipOnWindows(t)
	// Skip if test plugin doesn't exist
	pluginPath := getTestPluginPath("shell_fail_plugin.sh")
	if _, err := os.Stat(pluginPath); os.IsNotExist(err) {
		t.Skip("Test plugin not found:", pluginPath)
	}

	config := pluginapi.PluginConfig{
		"command": pluginPath,
	}

	plugin, err := NewShellPlugin(config)
	if err != nil {
		t.Fatalf("NewShellPlugin() error = %v", err)
	}

	ctx := context.Background()

	// Try to set - should fail with plugin error
	err = plugin.Set(ctx, pluginapi.PluginRequest{Key: "testkey", Value: "testvalue"})
	if err == nil {
		t.Error("Set() should return error when plugin fails")
	}

	// Error should include stderr
	if err != nil {
		errMsg := err.Error()
		if errMsg == "" {
			t.Error("Set() error message should not be empty")
		}
	}
}

func TestShellPlugin_WithArgs(t *testing.T) {
	skipOnWindows(t)
	config := pluginapi.PluginConfig{
		"command": "/bin/sh",
		"args":    []string{"-c", "eval \"$(cat)\" && echo \"$STUNMESH_KEY\""},
	}

	plugin, err := NewShellPlugin(config)
	if err != nil {
		t.Fatalf("NewShellPlugin() error = %v", err)
	}

	shellPlugin := plugin.(*ShellPlugin)
	if len(shellPlugin.args) != 2 {
		t.Errorf("ShellPlugin.args length = %d, want 2", len(shellPlugin.args))
	}

	ctx := context.Background()

	// Should succeed and echo the key
	value, err := plugin.Get(ctx, pluginapi.PluginRequest{Key: "testkey123"})
	if err != nil {
		t.Errorf("Get() error = %v, want nil", err)
	}

	if value != "testkey123" {
		t.Errorf("Get() = %q, want %q", value, "testkey123")
	}
}

func TestShellPlugin_EmptyStdout(t *testing.T) {
	skipOnWindows(t)
	config := pluginapi.PluginConfig{
		"command": "/bin/sh",
		"args":    []string{"-c", "exit 0"},
	}

	plugin, err := NewShellPlugin(config)
	if err != nil {
		t.Fatalf("NewShellPlugin() error = %v", err)
	}

	ctx := context.Background()

	// Should succeed but return empty string
	value, err := plugin.Get(ctx, pluginapi.PluginRequest{Key: "testkey"})
	if err != nil {
		t.Errorf("Get() error = %v, want nil", err)
	}

	if value != "" {
		t.Errorf("Get() = %q, want empty string", value)
	}
}

func TestShellPlugin_StdinVariables(t *testing.T) {
	skipOnWindows(t)
	// Test that shell variables are properly sent via stdin
	config := pluginapi.PluginConfig{
		"command": "/bin/sh",
		"args": []string{"-c", `
			eval "$(cat)"
			if [ "$STUNMESH_ACTION" != "get" ]; then
				echo "wrong action" >&2
				exit 1
			fi
			if [ "$STUNMESH_KEY" != "mykey" ]; then
				echo "wrong key" >&2
				exit 1
			fi
			echo "success"
		`},
	}

	plugin, err := NewShellPlugin(config)
	if err != nil {
		t.Fatalf("NewShellPlugin() error = %v", err)
	}

	ctx := context.Background()

	// Should succeed and return "success"
	value, err := plugin.Get(ctx, pluginapi.PluginRequest{Key: "mykey"})
	if err != nil {
		t.Errorf("Get() error = %v", err)
	}

	if value != "success" {
		t.Errorf("Get() = %q, want %q", value, "success")
	}
}

func TestShellPlugin_SetStdinVariables(t *testing.T) {
	skipOnWindows(t)
	// Test that shell variables including value are properly sent via stdin for Set
	config := pluginapi.PluginConfig{
		"command": "/bin/sh",
		"args": []string{"-c", `
			eval "$(cat)"
			if [ "$STUNMESH_ACTION" != "set" ]; then
				echo "wrong action: $STUNMESH_ACTION" >&2
				exit 1
			fi
			if [ "$STUNMESH_KEY" != "mykey" ]; then
				echo "wrong key: $STUNMESH_KEY" >&2
				exit 1
			fi
			if [ "$STUNMESH_VALUE" != "myvalue" ]; then
				echo "wrong value: $STUNMESH_VALUE" >&2
				exit 1
			fi
			exit 0
		`},
	}

	plugin, err := NewShellPlugin(config)
	if err != nil {
		t.Fatalf("NewShellPlugin() error = %v", err)
	}

	ctx := context.Background()

	// Should succeed
	err = plugin.Set(ctx, pluginapi.PluginRequest{Key: "mykey", Value: "myvalue"})
	if err != nil {
		t.Errorf("Set() error = %v, want nil", err)
	}
}

func TestShellPlugin_IdentityVariables(t *testing.T) {
	skipOnWindows(t)
	device := "it's; echo pwned $HOME wg0"
	req := testRequest("k", device)
	want := device + "\n" + req.LocalPublicKey.String() + "\n" + req.PeerPublicKey.String()

	for name, load := range map[string]string{
		"source": ". /dev/stdin",
		"eval":   `eval "$(cat)"`,
	} {
		t.Run(name, func(t *testing.T) {
			plugin, err := NewShellPlugin(pluginapi.PluginConfig{
				"command": "/bin/sh",
				"args": []string{"-c", load + `
					printf '%s\n%s\n%s' "$STUNMESH_DEVICE" "$STUNMESH_LOCAL_PUBLIC_KEY" "$STUNMESH_PEER_PUBLIC_KEY"`},
			})
			if err != nil {
				t.Fatalf("NewShellPlugin() error = %v", err)
			}
			got, err := plugin.Get(context.Background(), req)
			if err != nil {
				t.Fatalf("Get() error = %v", err)
			}
			if got != want {
				t.Errorf("Get() = %q, want %q", got, want)
			}
		})
	}
}

func TestShellPlugin_DeviceControlCharactersDropped(t *testing.T) {
	skipOnWindows(t)
	forgedKey := "0000000000000000000000000000000000000000"
	req := testRequest("realkey", "a\nSTUNMESH_KEY="+forgedKey+"\r\x7fb")

	t.Run("source", func(t *testing.T) {
		plugin, err := NewShellPlugin(pluginapi.PluginConfig{
			"command": "/bin/sh",
			"args":    []string{"-c", `. /dev/stdin; printf '%s|%s' "$STUNMESH_KEY" "$STUNMESH_DEVICE"`},
		})
		if err != nil {
			t.Fatalf("NewShellPlugin() error = %v", err)
		}
		got, err := plugin.Get(context.Background(), req)
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if want := "realkey|aSTUNMESH_KEY=" + forgedKey + "b"; got != want {
			t.Errorf("Get() = %q, want %q", got, want)
		}
	})

	t.Run("line parser", func(t *testing.T) {
		plugin, err := NewShellPlugin(pluginapi.PluginConfig{
			"command": "/bin/sh",
			"args": []string{"-c", `
				n=0
				while IFS= read -r line; do
					case "$line" in
					STUNMESH_KEY=*) key=${line#STUNMESH_KEY=} ;;
					esac
					case "$line" in
					STUNMESH_*) n=$((n+1)) ;;
					esac
				done
				printf '%s|%s' "$n" "$key"`},
		})
		if err != nil {
			t.Fatalf("NewShellPlugin() error = %v", err)
		}
		got, err := plugin.Get(context.Background(), req)
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if want := "5|realkey"; got != want {
			t.Errorf("Get() = %q, want %q", got, want)
		}
	})
}

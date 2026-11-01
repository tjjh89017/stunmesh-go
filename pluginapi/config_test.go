package pluginapi_test

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	pluginapi "github.com/tjjh89017/stunmesh-go/pluginapi"
)

type testConfig struct {
	Zone    string        `mapstructure:"zone"`
	Stores  []string      `mapstructure:"stores"`
	Timeout time.Duration `mapstructure:"timeout"`
}

func TestDecodeConfig(t *testing.T) {
	var cfg testConfig
	err := pluginapi.DecodeConfig(context.Background(), pluginapi.PluginConfig{
		"zone":    "example.com",
		"stores":  []interface{}{"a", "b"},
		"timeout": "10s",
	}, &cfg)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := testConfig{Zone: "example.com", Stores: []string{"a", "b"}, Timeout: 10 * time.Second}
	if !reflect.DeepEqual(cfg, want) {
		t.Errorf("got %+v, want %+v", cfg, want)
	}
}

func captureLog() (context.Context, *bytes.Buffer) {
	var buf bytes.Buffer
	logger := zerolog.New(&buf)
	return logger.WithContext(context.Background()), &buf
}

func TestDecodeConfigIgnoresReservedKeys(t *testing.T) {
	ctx, buf := captureLog()
	var cfg testConfig
	config := pluginapi.PluginConfig{
		"type":  "builtin",
		"name":  "x",
		"dedup": true,
		"zone":  "example.com",
	}
	if err := pluginapi.DecodeConfig(ctx, config, &cfg); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if cfg.Zone != "example.com" {
		t.Errorf("zone = %q", cfg.Zone)
	}
	if buf.Len() != 0 {
		t.Errorf("reserved keys must not warn, got: %s", buf.String())
	}
}

func TestDecodeConfigUnknownKeyWarns(t *testing.T) {
	ctx, buf := captureLog()
	var cfg testConfig
	err := pluginapi.DecodeConfig(ctx, pluginapi.PluginConfig{"zone": "example.com", "nxet": "a", "abc": 1}, &cfg)
	if err != nil {
		t.Fatalf("an unknown key must not fail the decode: %v", err)
	}
	if cfg.Zone != "example.com" {
		t.Errorf("zone = %q", cfg.Zone)
	}
	out := buf.String()
	if !strings.Contains(out, `"level":"warn"`) || !strings.Contains(out, `"keys":["abc","nxet"]`) {
		t.Errorf("expected a sorted warning naming the keys, got: %s", out)
	}
}

func TestDecodeConfigWrongType(t *testing.T) {
	var cfg testConfig
	if err := pluginapi.DecodeConfig(context.Background(), pluginapi.PluginConfig{"stores": "a"}, &cfg); err == nil {
		t.Fatal("expected an error for a string where a list is required")
	}
}

func TestDecodeConfigNil(t *testing.T) {
	var cfg testConfig
	if err := pluginapi.DecodeConfig(context.Background(), nil, &cfg); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !reflect.DeepEqual(cfg, testConfig{}) {
		t.Errorf("got %+v, want zero value", cfg)
	}
}

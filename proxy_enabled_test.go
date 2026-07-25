//go:build windows || wgproxy

package main

import (
	"runtime"
	"testing"

	"github.com/rs/zerolog"
	"github.com/tjjh89017/stunmesh-go/internal/config"
)

// Without proxy.listen, proxy mode is active only on Windows.
func TestProxyModeEnabled_NoListenKey(t *testing.T) {
	logger := zerolog.Nop()
	cfg := &config.Config{Interfaces: config.Interfaces{
		"wg0": {},
	}}

	got := proxyModeEnabled(cfg, &logger)
	want := runtime.GOOS == "windows"
	if got != want {
		t.Fatalf("proxyModeEnabled = %v, want %v on %s", got, want, runtime.GOOS)
	}
}

func TestProxyModeEnabled_ListenKeyActivates(t *testing.T) {
	logger := zerolog.Nop()
	cfg := &config.Config{Interfaces: config.Interfaces{
		"wg0": {Proxy: config.Proxy{Listen: 51999}},
	}}

	if !proxyModeEnabled(cfg, &logger) {
		t.Fatal("proxyModeEnabled = false with proxy.listen set, want true")
	}
}

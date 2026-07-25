package main

import (
	"runtime"
	"testing"

	"github.com/rs/zerolog"
	"github.com/tjjh89017/stunmesh-go/internal/config"
)

// KR3 guard: with no proxy.listen key, proxy mode is active only on Windows —
// on every other GOOS the plain wg client path is taken.
func TestProvideProxyMode_NoListenKey(t *testing.T) {
	logger := zerolog.Nop()
	cfg := &config.Config{Interfaces: config.Interfaces{
		"wg0": {},
	}}

	got := provideProxyMode(cfg, &logger)
	want := proxyMode(runtime.GOOS == "windows")
	if got != want {
		t.Fatalf("provideProxyMode = %v, want %v on %s", got, want, runtime.GOOS)
	}
}

func TestProvideProxyMode_ListenKeyActivates(t *testing.T) {
	logger := zerolog.Nop()
	cfg := &config.Config{Interfaces: config.Interfaces{
		"wg0": {Proxy: config.Proxy{Listen: 51999}},
	}}

	if got := provideProxyMode(cfg, &logger); !bool(got) {
		t.Fatal("provideProxyMode = false with proxy.listen set, want true")
	}
}

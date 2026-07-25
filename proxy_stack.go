package main

import (
	"github.com/tjjh89017/stunmesh-go/internal/stun"
	"github.com/tjjh89017/stunmesh-go/internal/wg"
)

// proxyStack bundles the two components whose construction differs per build
// flavor; newProxyStack (proxy_enabled.go / proxy_disabled.go) shares one
// signature so wire_gen.go stays tag-independent and never imports wgproxy.
type proxyStack struct {
	Client   wg.Client
	Resolver *stun.Resolver
}

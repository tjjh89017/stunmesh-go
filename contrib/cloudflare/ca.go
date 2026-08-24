package main

// The embedded Mozilla roots activate only when the system provides no
// certificate store, so the Cloudflare API stays reachable on minimal
// systems (OpenWrt, scratch containers) without a ca-certificates package.
// Unlike the main binary this is unconditional: the plugin ships as a
// standalone binary for exactly those environments.
import _ "golang.org/x/crypto/x509roots/fallback"

//go:build tools

package mobile

// gomobile bind needs golang.org/x/mobile in the module graph, but no source
// file imports it, so `go mod tidy` drops it and the AAR build then fails.
// This blank import, excluded from every real build by the tools tag, keeps
// the requirement.
import _ "golang.org/x/mobile/bind"

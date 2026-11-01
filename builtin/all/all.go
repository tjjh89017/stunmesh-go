// Package all links in the built-in plugins the release binary ships.
// Import it for its side effect; a custom binary may import a subset of
// builtin/* instead. Building with -tags no_<name> drops that plugin from
// the set.
package all

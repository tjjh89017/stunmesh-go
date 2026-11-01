// Package pluginapi is the public contract between stunmesh and its storage
// plugins: the Store interface, the request a Store receives, the exec
// protocol types, and the registry built-in plugins register with.
//
// Plugins compiled into a stunmesh binary import it, as does the stunmesh
// core. It follows semantic versioning with the stunmesh-go module; a
// breaking change to it needs a new major version.
package pluginapi

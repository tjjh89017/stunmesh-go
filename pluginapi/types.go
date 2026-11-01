package pluginapi

// PluginConfig holds one plugin instance's configuration, minus "type".
// The core reserves "type", "name" (the built-in to use for type builtin)
// and "dedup" (read for every plugin type); every other key belongs to the
// plugin.
type PluginConfig map[string]interface{}

// PluginDefinition defines a plugin configuration from YAML
type PluginDefinition struct {
	Type   string       `mapstructure:"type"`
	Config PluginConfig `mapstructure:",remain"`
}

// Exec Plugin Protocol

const (
	OpSet = "set"
	OpGet = "get"
)

// ExecRequest is the JSON request format for exec plugins
type ExecRequest struct {
	Action         string `json:"action"`
	Key            string `json:"key"`
	Value          string `json:"value,omitempty"`
	Device         string `json:"device,omitempty"`
	LocalPublicKey string `json:"local_public_key,omitempty"`
	PeerPublicKey  string `json:"peer_public_key,omitempty"`
}

// ExecResponse is the JSON response format for exec plugins
type ExecResponse struct {
	Success bool   `json:"success"`
	Value   string `json:"value,omitempty"`
	Error   string `json:"error,omitempty"`
}

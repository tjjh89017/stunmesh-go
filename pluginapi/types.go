package pluginapi

// PluginConfig holds configuration for a plugin
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
	Action string `json:"action"`
	Key    string `json:"key"`
	Value  string `json:"value,omitempty"`
}

// ExecResponse is the JSON response format for exec plugins
type ExecResponse struct {
	Success bool   `json:"success"`
	Value   string `json:"value,omitempty"`
	Error   string `json:"error,omitempty"`
}

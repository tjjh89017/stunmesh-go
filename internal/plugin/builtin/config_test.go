package builtin

import (
	"testing"

	pluginapi "github.com/tjjh89017/stunmesh-go/pluginapi"
)

func TestConfig_GetString(t *testing.T) {
	tests := []struct {
		name      string
		config    pluginapi.PluginConfig
		key       string
		wantValue string
		wantOk    bool
	}{
		{
			name:      "existing string key",
			config:    pluginapi.PluginConfig{"testkey": "testvalue"},
			key:       "testkey",
			wantValue: "testvalue",
			wantOk:    true,
		},
		{
			name:      "nonexistent key",
			config:    pluginapi.PluginConfig{"other": "value"},
			key:       "testkey",
			wantValue: "",
			wantOk:    false,
		},
		{
			name:      "non-string value",
			config:    pluginapi.PluginConfig{"testkey": 123},
			key:       "testkey",
			wantValue: "",
			wantOk:    false,
		},
		{
			name:      "empty string value",
			config:    pluginapi.PluginConfig{"testkey": ""},
			key:       "testkey",
			wantValue: "",
			wantOk:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := NewConfig(tt.config)
			gotValue, gotOk := cfg.GetString(tt.key)

			if gotValue != tt.wantValue {
				t.Errorf("GetString() value = %q, want %q", gotValue, tt.wantValue)
			}

			if gotOk != tt.wantOk {
				t.Errorf("GetString() ok = %v, want %v", gotOk, tt.wantOk)
			}
		})
	}
}

func TestConfig_GetStringRequired(t *testing.T) {
	tests := []struct {
		name    string
		config  pluginapi.PluginConfig
		key     string
		want    string
		wantErr bool
	}{
		{
			name:    "existing string key",
			config:  pluginapi.PluginConfig{"testkey": "testvalue"},
			key:     "testkey",
			want:    "testvalue",
			wantErr: false,
		},
		{
			name:    "nonexistent key",
			config:  pluginapi.PluginConfig{"other": "value"},
			key:     "testkey",
			want:    "",
			wantErr: true,
		},
		{
			name:    "non-string value",
			config:  pluginapi.PluginConfig{"testkey": 123},
			key:     "testkey",
			want:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := NewConfig(tt.config)
			got, err := cfg.GetStringRequired(tt.key)

			if (err != nil) != tt.wantErr {
				t.Errorf("GetStringRequired() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if got != tt.want {
				t.Errorf("GetStringRequired() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestConfig_GetStringSlice(t *testing.T) {
	tests := []struct {
		name    string
		config  pluginapi.PluginConfig
		key     string
		want    []string
		wantErr bool
	}{
		{
			name:   "missing key returns nil",
			config: pluginapi.PluginConfig{},
			key:    "list",
			want:   nil,
		},
		{
			name:   "[]string value",
			config: pluginapi.PluginConfig{"list": []string{"a", "b"}},
			key:    "list",
			want:   []string{"a", "b"},
		},
		{
			name:   "[]interface{} of strings",
			config: pluginapi.PluginConfig{"list": []interface{}{"a", "b"}},
			key:    "list",
			want:   []string{"a", "b"},
		},
		{
			name:    "[]interface{} with non-string entry",
			config:  pluginapi.PluginConfig{"list": []interface{}{"a", 1}},
			key:     "list",
			wantErr: true,
		},
		{
			name:    "non-list value",
			config:  pluginapi.PluginConfig{"list": "a"},
			key:     "list",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := NewConfig(tt.config)
			got, err := cfg.GetStringSlice(tt.key)

			if (err != nil) != tt.wantErr {
				t.Errorf("GetStringSlice() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && len(got) != len(tt.want) {
				t.Errorf("GetStringSlice() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConfig_GetDuration(t *testing.T) {
	tests := []struct {
		name    string
		config  pluginapi.PluginConfig
		key     string
		want    int64 // nanoseconds
		wantOk  bool
		wantErr bool
	}{
		{
			name:   "missing key",
			config: pluginapi.PluginConfig{},
			key:    "timeout",
			wantOk: false,
		},
		{
			name:   "duration string",
			config: pluginapi.PluginConfig{"timeout": "20s"},
			key:    "timeout",
			want:   int64(20e9),
			wantOk: true,
		},
		{
			name:   "int seconds",
			config: pluginapi.PluginConfig{"timeout": 20},
			key:    "timeout",
			want:   int64(20e9),
			wantOk: true,
		},
		{
			name:   "float64 seconds",
			config: pluginapi.PluginConfig{"timeout": 1.5},
			key:    "timeout",
			want:   int64(1.5e9),
			wantOk: true,
		},
		{
			name:    "invalid duration string",
			config:  pluginapi.PluginConfig{"timeout": "not-a-duration"},
			key:     "timeout",
			wantErr: true,
		},
		{
			name:    "unsupported type",
			config:  pluginapi.PluginConfig{"timeout": true},
			key:     "timeout",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := NewConfig(tt.config)
			got, ok, err := cfg.GetDuration(tt.key)

			if (err != nil) != tt.wantErr {
				t.Errorf("GetDuration() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr {
				return
			}

			if ok != tt.wantOk {
				t.Errorf("GetDuration() ok = %v, want %v", ok, tt.wantOk)
			}

			if ok && int64(got) != tt.want {
				t.Errorf("GetDuration() = %v, want %v", int64(got), tt.want)
			}
		})
	}
}

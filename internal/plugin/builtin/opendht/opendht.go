//go:build builtin_opendht

package opendht

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/tjjh89017/stunmesh-go/internal/plugin/registry"
	pluginapi "github.com/tjjh89017/stunmesh-go/pluginapi"
)

func init() {
	registry.Register("opendht", NewOpenDHTPlugin)
}

const (
	defaultMagic   = "stunmesh-v1"
	defaultTimeout = 15 * time.Second

	// Configuration keys
	configKeyEndpoint  = "endpoint"
	configKeyEndpoints = "endpoints"
	configKeyMagic     = "magic"
	configKeyTimeout   = "timeout"
)

// An OpenDHT key is an InfoHash: 160 bits, i.e. 40 hex characters. stunmesh
// keys are SHA1 hex, so they are used as-is -- but reject anything else
// rather than let the proxy interpret a bad path segment.
var keyPattern = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)

// OpenDHTPlugin implements the Store interface
type OpenDHTPlugin struct {
	endpoints []string
	magic     string
	client    *http.Client
}

// envelope wraps the value stored under a key.
//
// A key holds a set of values rather than a single overwritable slot, and
// anyone may publish under a key they know, so a stored value cannot be
// assumed to be ours. Publishing every refresh cycle against OpenDHT's
// 10-minute expiry also leaves several of our own values under a key at once,
// returned in no particular order -- so Ts is what tells them apart, not just
// a tie-breaker.
type envelope struct {
	Magic string `json:"magic"`
	Ts    int64  `json:"ts"`
	Data  string `json:"data"`
}

// value is one entry as the proxy reports it. Get returns them as
// newline-delimited JSON, one object per line.
type value struct {
	Data string `json:"data"`
}

// BuiltinConfig helper
type BuiltinConfig struct {
	config pluginapi.PluginConfig
}

func (c *BuiltinConfig) GetString(key string) (string, bool) {
	val, ok := c.config[key]
	if !ok {
		return "", false
	}
	str, ok := val.(string)
	return str, ok
}

// GetStringSlice reads a list that YAML may deliver as []interface{}, or that
// mapstructure's weak typing may have already turned into []string.
func (c *BuiltinConfig) GetStringSlice(key string) ([]string, error) {
	val, ok := c.config[key]
	if !ok {
		return nil, nil
	}

	switch v := val.(type) {
	case []string:
		return v, nil
	case []interface{}:
		items := make([]string, 0, len(v))
		for _, item := range v {
			str, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("%s must be a list of strings", key)
			}
			items = append(items, str)
		}
		return items, nil
	default:
		return nil, fmt.Errorf("%s must be a list of strings", key)
	}
}

// GetDuration reads a timeout expressed either as a duration string such as
// "20s" or as a plain number of seconds, since a YAML scalar may arrive as
// either depending on how it was written.
func (c *BuiltinConfig) GetDuration(key string) (time.Duration, bool, error) {
	val, ok := c.config[key]
	if !ok {
		return 0, false, nil
	}

	switch v := val.(type) {
	case string:
		d, err := time.ParseDuration(v)
		if err != nil {
			return 0, false, fmt.Errorf("%s is not a valid duration: %w", key, err)
		}
		return d, true, nil
	case int:
		return time.Duration(v) * time.Second, true, nil
	case float64:
		return time.Duration(v * float64(time.Second)), true, nil
	default:
		return 0, false, fmt.Errorf("%s must be a duration or a number of seconds", key)
	}
}

// normalizeEndpoint requires an explicit http:// or https:// scheme. Go's http
// client rejects a bare host with "unsupported protocol scheme", but only once
// a request is made -- every refresh cycle, long after startup. curl would
// instead assume http, which for a scheme-less "dhtproxy.jami.net" is a silent
// downgrade from the https the default endpoint uses. Neither is a good answer
// to a value that simply does not say what it means.
func normalizeEndpoint(endpoint string) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", fmt.Errorf("opendht endpoint %q must start with http:// or https://", endpoint)
	}

	// doRequest joins with "/key/...", so a trailing slash would double it.
	return strings.TrimRight(endpoint, "/"), nil
}

// resolveEndpoints merges the singular endpoint in front of the endpoints list
// and deduplicates preserving order, the same shape as stun.address feeding
// stun.addresses. Every entry is validated here so a typo in the third one is
// not discovered only when the first two happen to be down.
func resolveEndpoints(cfg *BuiltinConfig) ([]string, error) {
	list, err := cfg.GetStringSlice(configKeyEndpoints)
	if err != nil {
		return nil, err
	}

	single, _ := cfg.GetString(configKeyEndpoint)

	seen := make(map[string]struct{})
	endpoints := make([]string, 0, len(list)+1)
	for _, endpoint := range append([]string{single}, list...) {
		if endpoint == "" {
			continue
		}

		normalized, err := normalizeEndpoint(endpoint)
		if err != nil {
			return nil, err
		}

		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		endpoints = append(endpoints, normalized)
	}

	// No default: which proxy to trust is a decision for whoever runs the
	// mesh, not something to inherit silently. The plugin README suggests some.
	if len(endpoints) == 0 {
		return nil, fmt.Errorf("opendht needs %s or %s; see the plugin README for suggested proxies", configKeyEndpoint, configKeyEndpoints)
	}

	return endpoints, nil
}

// NewOpenDHTPlugin creates a new OpenDHT plugin instance
func NewOpenDHTPlugin(config pluginapi.PluginConfig) (pluginapi.Store, error) {
	cfg := &BuiltinConfig{config: config}

	endpoints, err := resolveEndpoints(cfg)
	if err != nil {
		return nil, err
	}

	magic, ok := cfg.GetString(configKeyMagic)
	if !ok || magic == "" {
		magic = defaultMagic
	}

	timeout, ok, err := cfg.GetDuration(configKeyTimeout)
	if err != nil {
		return nil, err
	}
	if !ok {
		timeout = defaultTimeout
	}

	client := &http.Client{
		// A lookup that finds nothing legitimately takes several seconds to
		// converge, so a short timeout turns a slow success into a false
		// "not found".
		Timeout: timeout,
		Transport: &http.Transport{
			MaxIdleConns:        2,
			MaxIdleConnsPerHost: 2,
			IdleConnTimeout:     30 * time.Second,
		},
	}

	return &OpenDHTPlugin{
		endpoints: endpoints,
		magic:     magic,
		client:    client,
	}, nil
}

// doRequest tries each endpoint in order and returns the first success. Only a
// failed request moves on to the next: a request that succeeds but carries no
// value for the key is an answer, not a failure, and every endpoint fronts the
// same DHT so asking another would give the same one.
func (p *OpenDHTPlugin) doRequest(ctx context.Context, method, key string, body []byte) ([]byte, error) {
	logger := zerolog.Ctx(ctx)

	var errs []error
	for _, endpoint := range p.endpoints {
		data, err := p.doRequestTo(ctx, endpoint, method, key, body)
		if err == nil {
			return data, nil
		}

		logger.Warn().Err(err).Str("endpoint", endpoint).Msg("opendht endpoint failed, trying next")
		errs = append(errs, fmt.Errorf("%s: %w", endpoint, err))
	}

	return nil, errors.Join(errs...)
}

func (p *OpenDHTPlugin) doRequestTo(ctx context.Context, endpoint, method, key string, body []byte) ([]byte, error) {
	url := fmt.Sprintf("%s/key/%s", endpoint, key)

	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("API error: %s - %s", resp.Status, string(data))
	}

	return data, nil
}

// Get retrieves a value from OpenDHT
func (p *OpenDHTPlugin) Get(ctx context.Context, key string) (string, error) {
	logger := zerolog.Ctx(ctx)
	logger.Info().Str("key", key).Msg("get data from builtin opendht plugin")

	if !keyPattern.MatchString(key) {
		return "", fmt.Errorf("key must be 40 hex characters: %s", key)
	}

	data, err := p.doRequest(ctx, http.MethodGet, key, nil)
	if err != nil {
		return "", err
	}

	// Keep the entries carrying our magic and return the most recent. Values
	// that are not our envelope -- or not JSON at all -- are ignored, which
	// also absorbs whatever a third party publishes under the same key.
	var newest *envelope
	for _, line := range bytes.Split(data, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}

		var v value
		if err := json.Unmarshal(line, &v); err != nil {
			continue
		}

		raw, err := base64.StdEncoding.DecodeString(v.Data)
		if err != nil {
			continue
		}

		var e envelope
		if err := json.Unmarshal(raw, &e); err != nil {
			continue
		}

		if e.Magic != p.magic {
			continue
		}

		if newest == nil || e.Ts > newest.Ts {
			found := e
			newest = &found
		}
	}

	if newest == nil {
		return "", fmt.Errorf("no value found for key: %s", key)
	}

	return newest.Data, nil
}

// Set stores a value in OpenDHT
func (p *OpenDHTPlugin) Set(ctx context.Context, key string, value string) error {
	logger := zerolog.Ctx(ctx)
	logger.Info().Str("key", key).Msg("set data to builtin opendht plugin")

	if !keyPattern.MatchString(key) {
		return fmt.Errorf("key must be 40 hex characters: %s", key)
	}

	payload, err := json.Marshal(&envelope{
		Magic: p.magic,
		Ts:    time.Now().Unix(),
		Data:  value,
	})
	if err != nil {
		return err
	}

	body, err := json.Marshal(map[string]string{
		"data": base64.StdEncoding.EncodeToString(payload),
	})
	if err != nil {
		return err
	}

	_, err = p.doRequest(ctx, http.MethodPost, key, body)
	return err
}

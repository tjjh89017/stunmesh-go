//go:build builtin_cloudflare

package cloudflare

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/rs/zerolog"
)

const cfAPI = "https://api.cloudflare.com/client/v4"

// Store interface (copied to avoid import cycle)
type Store interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key string, value string) error
}

// PluginConfig type
type PluginConfig map[string]interface{}

// CloudflarePlugin implements the Store interface
type CloudflarePlugin struct {
	token     string
	zoneID    string
	zoneName  string
	subdomain string
	client    *http.Client
}

// Minimal JSON response structures (only fields we need)
type cfResponse struct {
	Success bool              `json:"success"`
	Result  []json.RawMessage `json:"result,omitempty"`
	Errors  []cfError         `json:"errors,omitempty"`
}

type cfError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// BuiltinConfig helper
type BuiltinConfig struct {
	config PluginConfig
}

func (c *BuiltinConfig) GetString(key string) (string, bool) {
	val, ok := c.config[key]
	if !ok {
		return "", false
	}
	str, ok := val.(string)
	return str, ok
}

func (c *BuiltinConfig) GetStringRequired(key string) (string, error) {
	val, ok := c.GetString(key)
	if !ok {
		return "", fmt.Errorf("%s is required", key)
	}
	return val, nil
}

// NewCloudflarePlugin creates a new Cloudflare plugin instance
func NewCloudflarePlugin(config PluginConfig) (Store, error) {
	cfg := &BuiltinConfig{config: config}

	zoneName, err := cfg.GetStringRequired("zone_name")
	if err != nil {
		return nil, err
	}

	token, err := cfg.GetStringRequired("api_token")
	if err != nil {
		return nil, err
	}

	subdomain, _ := cfg.GetString("subdomain")

	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        2,
			MaxIdleConnsPerHost: 2,
			IdleConnTimeout:     30 * time.Second,
		},
	}

	p := &CloudflarePlugin{
		token:     token,
		zoneName:  zoneName,
		subdomain: subdomain,
		client:    client,
	}

	// Get zone ID during initialization
	ctx := context.Background()
	zoneID, err := p.getZoneID(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get zone ID: %w", err)
	}
	p.zoneID = zoneID

	return p, nil
}

func (p *CloudflarePlugin) doRequest(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	url := cfAPI + path

	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+p.token)
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

func (p *CloudflarePlugin) getZoneID(ctx context.Context) (string, error) {
	data, err := p.doRequest(ctx, "GET", "/zones?name="+p.zoneName, nil)
	if err != nil {
		return "", err
	}

	var resp cfResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", err
	}

	if !resp.Success || len(resp.Result) == 0 {
		return "", fmt.Errorf("zone not found: %s", p.zoneName)
	}

	// Extract ID from first result
	var result struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(resp.Result[0], &result); err != nil {
		return "", err
	}

	return result.ID, nil
}

func (p *CloudflarePlugin) getRecordName(key string) string {
	// Key is already SHA1 hex from stunmesh
	if p.subdomain != "" {
		return fmt.Sprintf("%s.%s.%s", key, p.subdomain, p.zoneName)
	}
	return fmt.Sprintf("%s.%s", key, p.zoneName)
}

func (p *CloudflarePlugin) findRecord(ctx context.Context, name string) (string, string, error) {
	path := fmt.Sprintf("/zones/%s/dns_records?type=TXT&name=%s", p.zoneID, name)
	data, err := p.doRequest(ctx, "GET", path, nil)
	if err != nil {
		return "", "", err
	}

	var resp cfResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", "", err
	}

	if !resp.Success || len(resp.Result) == 0 {
		return "", "", nil // Not found
	}

	var record struct {
		ID      string `json:"id"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(resp.Result[0], &record); err != nil {
		return "", "", err
	}

	return record.ID, record.Content, nil
}

// Get retrieves a value from Cloudflare DNS
func (p *CloudflarePlugin) Get(ctx context.Context, key string) (string, error) {
	logger := zerolog.Ctx(ctx)
	logger.Info().Str("key", key).Msg("get data from builtin cloudflare plugin")

	name := p.getRecordName(key)
	_, content, err := p.findRecord(ctx, name)
	if err != nil {
		return "", err
	}
	if content == "" {
		return "", fmt.Errorf("record not found: %s", name)
	}
	return content, nil
}

// Set stores a value in Cloudflare DNS
func (p *CloudflarePlugin) Set(ctx context.Context, key string, value string) error {
	logger := zerolog.Ctx(ctx)
	logger.Info().Str("key", key).Msg("set data to builtin cloudflare plugin")

	name := p.getRecordName(key)
	recordID, existingContent, err := p.findRecord(ctx, name)
	if err != nil {
		return err
	}

	// Skip if content unchanged
	if existingContent == value {
		return nil
	}

	if recordID != "" {
		// Update existing record
		body := fmt.Sprintf(`{"content":"%s"}`, value)
		path := fmt.Sprintf("/zones/%s/dns_records/%s", p.zoneID, recordID)
		_, err = p.doRequest(ctx, "PATCH", path, []byte(body))
		return err
	}

	// Create new record
	body := fmt.Sprintf(`{"type":"TXT","name":"%s","content":"%s","comment":"Stunmesh"}`, name, value)
	path := fmt.Sprintf("/zones/%s/dns_records", p.zoneID)
	_, err = p.doRequest(ctx, "POST", path, []byte(body))
	return err
}

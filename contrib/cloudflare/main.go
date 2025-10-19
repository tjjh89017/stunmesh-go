package main

import (
	"context"
	"crypto/sha1"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/cloudflare/cloudflare-go"
	"github.com/tjjh89017/stunmesh-go/plugin"
)

// CloudflarePlugin manages DNS TXT records in Cloudflare
type CloudflarePlugin struct {
	api       *cloudflare.API
	zoneID    *cloudflare.ResourceContainer
	zoneName  string
	subdomain string
}

// NewCloudflarePlugin creates a new Cloudflare plugin instance
func NewCloudflarePlugin(zoneName, apiToken, subdomain string) (*CloudflarePlugin, error) {
	if zoneName == "" {
		return nil, fmt.Errorf("zone_name is required")
	}

	if apiToken == "" {
		return nil, fmt.Errorf("api_token is required")
	}

	api, err := cloudflare.NewWithAPIToken(apiToken)
	if err != nil {
		return nil, fmt.Errorf("failed to create cloudflare API client: %w", err)
	}

	zoneID, err := api.ZoneIDByName(zoneName)
	if err != nil {
		return nil, fmt.Errorf("failed to get zone ID for %s: %w", zoneName, err)
	}

	return &CloudflarePlugin{
		api:       api,
		zoneID:    &cloudflare.ResourceContainer{Identifier: zoneID},
		zoneName:  zoneName,
		subdomain: subdomain,
	}, nil
}

// Get retrieves endpoint data from Cloudflare DNS TXT records
func (p *CloudflarePlugin) Get(ctx context.Context, key string) (string, error) {
	records, resultInfo, err := p.associatedRecords(ctx, key)
	if err != nil {
		return "", err
	}

	if resultInfo.Count == 0 {
		return "", fmt.Errorf("endpoint data not found for key %s", key)
	}

	return records[0].Content, nil
}

// Set stores/updates endpoint data in Cloudflare DNS TXT records
func (p *CloudflarePlugin) Set(ctx context.Context, key string, value string) error {
	records, resultInfo, err := p.associatedRecords(ctx, key)
	if err != nil {
		return err
	}

	recordName := p.getRecordName(key)

	// If no records exist, create one
	if resultInfo.Count == 0 {
		_, err := p.api.CreateDNSRecord(ctx, p.zoneID, cloudflare.CreateDNSRecordParams{
			Type:    "TXT",
			Name:    recordName,
			Content: value,
			Comment: "Created by Stunmesh",
		})
		return err
	}

	// If there are duplicates, delete all but the first
	if resultInfo.Count > 1 {
		for _, record := range records[1:] {
			if err := p.api.DeleteDNSRecord(ctx, p.zoneID, record.ID); err != nil {
				continue // Continue even if delete fails
			}
		}
	}

	// Skip update if the same record exists
	if value == records[0].Content {
		return nil
	}

	// Update the first record
	_, err = p.api.UpdateDNSRecord(ctx, p.zoneID, cloudflare.UpdateDNSRecordParams{
		ID:      records[0].ID,
		Content: value,
	})

	return err
}

func (p *CloudflarePlugin) getRecordName(key string) string {
	// Hash the key to create a DNS-safe record name
	hash := sha1.Sum([]byte(key))
	hashedKey := fmt.Sprintf("%x", hash)

	if p.subdomain != "" {
		return fmt.Sprintf("%s.%s.%s", hashedKey, p.subdomain, p.zoneName)
	}
	return fmt.Sprintf("%s.%s", hashedKey, p.zoneName)
}

func (p *CloudflarePlugin) associatedRecords(ctx context.Context, key string) ([]cloudflare.DNSRecord, *cloudflare.ResultInfo, error) {
	recordName := p.getRecordName(key)
	return p.api.ListDNSRecords(ctx, p.zoneID, cloudflare.ListDNSRecordsParams{
		Name: recordName,
		Type: "TXT",
	})
}

func main() {
	// Parse command line flags
	zoneName := flag.String("zone", "", "Cloudflare zone name (required)")
	apiToken := flag.String("token", "", "Cloudflare API token (required)")
	subdomain := flag.String("subdomain", "", "Optional subdomain prefix for DNS records")
	flag.Parse()

	// Initialize plugin
	cloudflarePlugin, err := NewCloudflarePlugin(*zoneName, *apiToken, *subdomain)
	if err != nil {
		respondError(err)
		os.Exit(1)
	}

	// Read request from stdin
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		respondError(fmt.Errorf("failed to read stdin: %w", err))
		os.Exit(1)
	}

	var req plugin.ExecRequest
	if err := json.Unmarshal(data, &req); err != nil {
		respondError(fmt.Errorf("failed to parse request: %w", err))
		os.Exit(1)
	}

	ctx := context.Background()

	// Handle action
	switch req.Action {
	case plugin.OpGet:
		value, err := cloudflarePlugin.Get(ctx, req.Key)
		if err != nil {
			respondError(err)
			os.Exit(1)
		}
		respondSuccess(value)

	case plugin.OpSet:
		if err := cloudflarePlugin.Set(ctx, req.Key, req.Value); err != nil {
			respondError(err)
			os.Exit(1)
		}
		respondSuccess("")

	default:
		respondError(fmt.Errorf("unknown action: %s", req.Action))
		os.Exit(1)
	}
}

func respondSuccess(value string) {
	resp := plugin.ExecResponse{
		Success: true,
		Value:   value,
	}
	json.NewEncoder(os.Stdout).Encode(resp)
}

func respondError(err error) {
	resp := plugin.ExecResponse{
		Success: false,
		Error:   err.Error(),
	}
	json.NewEncoder(os.Stdout).Encode(resp)
}

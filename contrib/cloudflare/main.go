package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/cloudflare/cloudflare-go"
	"github.com/tjjh89017/stunmesh-go/pluginapi"
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
	// key is already the SHA1 hex digest stunmesh derives per peer; hashing
	// it again here would diverge from the builtin plugin and cloudflare-shell.sh.
	if p.subdomain != "" {
		return fmt.Sprintf("%s.%s.%s", key, p.subdomain, p.zoneName)
	}
	return fmt.Sprintf("%s.%s", key, p.zoneName)
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
		respondError(os.Stdout, err)
		os.Exit(1)
	}

	os.Exit(handleRequest(context.Background(), cloudflarePlugin, os.Stdin, os.Stdout))
}

// handleRequest reads one exec-protocol request from stdin, dispatches it
// against store, and writes the response envelope to stdout. It is
// extracted from main() as a seam: store is already-constructed here, so
// tests can exercise stdin parsing and dispatch (including malformed JSON)
// against a fake store without going through NewCloudflarePlugin, which
// makes a real Cloudflare API call before stdin is ever read. Returns the
// process exit code main() should use.
func handleRequest(ctx context.Context, store pluginapi.Store, stdin io.Reader, stdout io.Writer) int {
	data, err := io.ReadAll(stdin)
	if err != nil {
		respondError(stdout, fmt.Errorf("failed to read stdin: %w", err))
		return 1
	}

	var req pluginapi.ExecRequest
	if err := json.Unmarshal(data, &req); err != nil {
		respondError(stdout, fmt.Errorf("failed to parse request: %w", err))
		return 1
	}

	switch req.Action {
	case pluginapi.OpGet:
		value, err := store.Get(ctx, req.Key)
		if err != nil {
			respondError(stdout, err)
			return 1
		}
		respondSuccess(stdout, value)

	case pluginapi.OpSet:
		if err := store.Set(ctx, req.Key, req.Value); err != nil {
			respondError(stdout, err)
			return 1
		}
		respondSuccess(stdout, "")

	default:
		respondError(stdout, fmt.Errorf("unknown action: %s", req.Action))
		return 1
	}

	return 0
}

func respondSuccess(w io.Writer, value string) {
	resp := pluginapi.ExecResponse{
		Success: true,
		Value:   value,
	}
	// A failed Encode here means the caller never gets a parsable envelope
	// at all (internal/plugin/exec.go's Decode then fails), so surface it
	// on stderr rather than let it pass unnoticed.
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		fmt.Fprintf(os.Stderr, "failed to encode response: %v\n", err)
		os.Exit(1)
	}
}

func respondError(w io.Writer, err error) {
	resp := pluginapi.ExecResponse{
		Success: false,
		Error:   err.Error(),
	}
	if encErr := json.NewEncoder(w).Encode(resp); encErr != nil {
		fmt.Fprintf(os.Stderr, "failed to encode error response: %v (original error: %v)\n", encErr, err)
		os.Exit(1)
	}
}

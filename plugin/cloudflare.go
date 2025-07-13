package plugin

import (
	"context"
	"fmt"

	"github.com/cloudflare/cloudflare-go"
	"github.com/mitchellh/mapstructure"
	"github.com/rs/zerolog"
)

type CloudflareConfig struct {
	ZoneName  string `mapstructure:"zone_name"`
	Subdomain string `mapstructure:"subdomain"`
	ApiToken  string `mapstructure:"api_token"`
}

type CloudflarePlugin struct {
	api       *cloudflare.API
	zoneId    *cloudflare.ResourceContainer
	zoneName  string
	subdomain string
}

func NewCloudflarePlugin(config PluginConfig) (Store, error) {
	var cfg CloudflareConfig
	if err := mapstructure.Decode(config, &cfg); err != nil {
		return nil, fmt.Errorf("failed to decode cloudflare config: %w", err)
	}

	if cfg.ZoneName == "" {
		return nil, fmt.Errorf("zone_name is required for cloudflare plugin")
	}

	if cfg.ApiToken == "" {
		return nil, fmt.Errorf("api_token is required for cloudflare plugin")
	}

	api, err := cloudflare.NewWithAPIToken(cfg.ApiToken)
	if err != nil {
		return nil, fmt.Errorf("failed to create cloudflare API client: %w", err)
	}

	zoneId, err := api.ZoneIDByName(cfg.ZoneName)
	if err != nil {
		return nil, fmt.Errorf("failed to get zone ID for %s: %w", cfg.ZoneName, err)
	}

	return &CloudflarePlugin{
		api:       api,
		zoneId:    &cloudflare.ResourceContainer{Identifier: zoneId},
		zoneName:  cfg.ZoneName,
		subdomain: cfg.Subdomain,
	}, nil
}

func (p *CloudflarePlugin) Get(ctx context.Context, key string) (string, error) {
	logger := zerolog.Ctx(ctx)
	logger.Info().Str("key", key).Msg("get IP info from Cloudflare")

	records, resultInfo, err := p.associatedRecords(ctx, key)
	if err != nil {
		return "", err
	}

	if resultInfo.Count == 0 {
		return "", fmt.Errorf("endpoint data not found for key %s", key)
	}

	return records[0].Content, nil
}

func (p *CloudflarePlugin) Set(ctx context.Context, key string, value string) error {
	logger := zerolog.Ctx(ctx)
	logger.Info().Str("key", key).Str("value", value).Msg("store IP info to Cloudflare")

	records, resultInfo, err := p.associatedRecords(ctx, key)
	if err != nil {
		return err
	}

	recordName := p.getRecordName(key)

	// If no records exist, create one
	if resultInfo.Count == 0 {
		_, err := p.api.CreateDNSRecord(ctx, p.zoneId, cloudflare.CreateDNSRecordParams{
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
			if err := p.api.DeleteDNSRecord(ctx, p.zoneId, record.ID); err != nil {
				continue // Continue even if delete fails
			}
		}
	}

	// Skip update if the same record exists
	if value == records[0].Content {
		logger.Info().Str("key", key).Str("value", value).Msg("the same record exists, skip the update.")
		return nil
	}

	// Update the first record
	_, err = p.api.UpdateDNSRecord(ctx, p.zoneId, cloudflare.UpdateDNSRecordParams{
		ID:      records[0].ID,
		Content: value,
	})

	return err
}

func (p *CloudflarePlugin) getRecordName(key string) string {
	if p.subdomain != "" {
		return fmt.Sprintf("%s.%s.%s", key, p.subdomain, p.zoneName)
	}
	return fmt.Sprintf("%s.%s", key, p.zoneName)
}

func (p *CloudflarePlugin) associatedRecords(ctx context.Context, key string) ([]cloudflare.DNSRecord, *cloudflare.ResultInfo, error) {
	recordName := p.getRecordName(key)
	return p.api.ListDNSRecords(ctx, p.zoneId, cloudflare.ListDNSRecordsParams{
		Name: recordName,
		Type: "TXT",
	})
}

package store

import (
	"context"
	"errors"
	"sync"

	"github.com/cloudflare/cloudflare-go"
	"github.com/rs/zerolog"
	"github.com/tjjh89017/stunmesh-go/plugin"
)

var (
	ErrEndpointDataNotFound = errors.New("endpoint data not found")
)

type CloudflareApi interface {
	ListDNSRecords(ctx context.Context, rc *cloudflare.ResourceContainer, params cloudflare.ListDNSRecordsParams) ([]cloudflare.DNSRecord, *cloudflare.ResultInfo, error)
	CreateDNSRecord(ctx context.Context, rc *cloudflare.ResourceContainer, params cloudflare.CreateDNSRecordParams) (cloudflare.DNSRecord, error)
	UpdateDNSRecord(ctx context.Context, rc *cloudflare.ResourceContainer, params cloudflare.UpdateDNSRecordParams) (cloudflare.DNSRecord, error)
	DeleteDNSRecord(ctx context.Context, rc *cloudflare.ResourceContainer, recordId string) error
	ZoneIDByName(zoneName string) (string, error)
}

var _ plugin.Store = &CloudflareStore{}

type CloudflareStore struct {
	mutex    sync.RWMutex
	api      CloudflareApi
	zoneId   *cloudflare.ResourceContainer
	zoneName string
}

func NewCloudflareStore(api CloudflareApi, zoneName string) *CloudflareStore {
	return &CloudflareStore{api: api, zoneName: zoneName}
}

func (s *CloudflareStore) Get(ctx context.Context, key string) (string, error) {
	logger := zerolog.Ctx(ctx)

	logger.Info().Str("key", key).Msg("get IP info from Cloudflare")
	records, info, err := s.associatedRecords(ctx, key)
	if err != nil {
		return "", err
	}

	isFound := info.Count > 0
	if !isFound {
		return "", ErrEndpointDataNotFound
	}

	return records[0].Content, nil
}

func (s *CloudflareStore) Set(ctx context.Context, key string, value string) error {
	logger := zerolog.Ctx(ctx)

	logger.Info().Str("key", key).Str("value", value).Msg("store IP info to Cloudflare")
	records, info, err := s.associatedRecords(ctx, key)
	if err != nil {
		return err
	}

	zoneId, err := s.ZoneId()
	if err != nil {
		return err
	}

	name := key + "." + s.zoneName

	isFound := info.Count > 0
	if !isFound {
		_, err := s.api.CreateDNSRecord(ctx, zoneId, cloudflare.CreateDNSRecordParams{
			Type:    "TXT",
			Name:    name,
			Content: value,
			Comment: "Created by Stunmesh",
		})

		return err
	}

	isDuplicate := info.Count > 1
	if isDuplicate {
		for _, x := range records[1:] {
			if err := s.api.DeleteDNSRecord(ctx, zoneId, x.ID); err != nil {
				continue
			}
		}
	}

	// skip update the same record
	if value == records[0].Content {
		logger.Info().Str("key", key).Str("value", value).Msg("the same record exists, skip the update.")
		return nil
	}

	_, err = s.api.UpdateDNSRecord(ctx, zoneId, cloudflare.UpdateDNSRecordParams{
		ID:      records[0].ID,
		Content: value,
	})

	return err
}

func (s *CloudflareStore) ZoneId() (*cloudflare.ResourceContainer, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if s.zoneId == nil {
		zone, err := s.api.ZoneIDByName(s.zoneName)
		if err != nil {
			return nil, err
		}

		s.zoneId = cloudflare.ZoneIdentifier(zone)
	}

	return s.zoneId, nil
}

func (s *CloudflareStore) associatedRecords(ctx context.Context, key string) ([]cloudflare.DNSRecord, *cloudflare.ResultInfo, error) {
	zoneId, err := s.ZoneId()
	if err != nil {
		return nil, nil, err
	}

	return s.api.ListDNSRecords(ctx, zoneId, cloudflare.ListDNSRecordsParams{
		Name: key + "." + s.zoneName,
		Type: "TXT",
	})
}

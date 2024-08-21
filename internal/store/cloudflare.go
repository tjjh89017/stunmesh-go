package store

import (
	"context"
	"errors"
	"sync"

	"github.com/cloudflare/cloudflare-go"
	"github.com/tjjh89017/stunmesh-go/plugin"
)

var (
	ErrEndpointDataNotFound = errors.New("endpoint data not found")
)

type CloudflareApi interface {
	DNSRecords(ctx context.Context, zoneId string, rr cloudflare.DNSRecord) ([]cloudflare.DNSRecord, error)
	CreateDNSRecord(ctx context.Context, zoneId string, rr cloudflare.DNSRecord) (*cloudflare.DNSRecordResponse, error)
	UpdateDNSRecord(ctx context.Context, zoneId, recordId string, rr cloudflare.DNSRecord) error
	DeleteDNSRecord(ctx context.Context, zoneId, recordId string) error
	ZoneIDByName(zoneName string) (string, error)
}

var _ plugin.Store = &CloudflareStore{}

type CloudflareStore struct {
	mutex    sync.RWMutex
	api      CloudflareApi
	zoneId   string
	zoneName string
}

func NewCloudflareStore(api CloudflareApi, zoneName string) *CloudflareStore {
	return &CloudflareStore{api: api, zoneName: zoneName}
}

func (s *CloudflareStore) Get(ctx context.Context, key string) (string, error) {
	records, err := s.associatedRecords(ctx, key)
	if err != nil {
		return "", err
	}

	isFound := len(records) > 0
	if !isFound {
		return "", ErrEndpointDataNotFound
	}

	return records[0].Content, nil
}

func (s *CloudflareStore) Set(ctx context.Context, key string, value string) error {
	records, err := s.associatedRecords(ctx, key)
	if err != nil {
		return err
	}

	zoneId, err := s.ZoneId()
	if err != nil {
		return err
	}

	record := cloudflare.DNSRecord{
		Type:    "TXT",
		Name:    key + "." + s.zoneName,
		Content: value,
	}

	isFound := len(records) > 0
	if !isFound {
		_, err := s.api.CreateDNSRecord(ctx, zoneId, record)
		return err
	}

	isDuplicate := len(records) > 1
	if isDuplicate {
		for _, x := range records[1:] {
			if err := s.api.DeleteDNSRecord(ctx, zoneId, x.ID); err != nil {
				continue
			}
		}
	}

	return s.api.UpdateDNSRecord(ctx, zoneId, records[0].ID, record)
}

func (s *CloudflareStore) ZoneId() (string, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if s.zoneId == "" {
		zone, err := s.api.ZoneIDByName(s.zoneName)
		if err != nil {
			return "", err
		}

		s.zoneId = zone
	}

	return s.zoneId, nil
}

func (s *CloudflareStore) associatedRecords(ctx context.Context, key string) ([]cloudflare.DNSRecord, error) {
	zoneId, err := s.ZoneId()
	if err != nil {
		return nil, err
	}

	return s.api.DNSRecords(ctx, zoneId, cloudflare.DNSRecord{Type: "TXT", Name: key + "." + s.zoneName})
}

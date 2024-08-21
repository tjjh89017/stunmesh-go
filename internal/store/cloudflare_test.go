package store_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/cloudflare/cloudflare-go"
	"github.com/tjjh89017/stunmesh-go/internal/store"
)

var _ store.CloudflareApi = &mockCloudflareApi{}

type mockCloudflareApi struct {
	mutex   sync.RWMutex
	lastId  int
	records []cloudflare.DNSRecord
}

func newMockCloudflareApi() *mockCloudflareApi {
	return &mockCloudflareApi{
		records: []cloudflare.DNSRecord{},
	}
}

func (m *mockCloudflareApi) DNSRecords(ctx context.Context, zoneId string, rr cloudflare.DNSRecord) ([]cloudflare.DNSRecord, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	matchedRecords := []cloudflare.DNSRecord{}

	for _, record := range m.records {
		isNameMatched := record.Name == rr.Name
		isTypeMatched := record.Type == rr.Type

		if isNameMatched && isTypeMatched {
			matchedRecords = append(matchedRecords, record)
		}
	}

	return matchedRecords, nil
}

func (m *mockCloudflareApi) CreateDNSRecord(ctx context.Context, zoneId string, rr cloudflare.DNSRecord) (*cloudflare.DNSRecordResponse, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.lastId++
	rr.ID = fmt.Sprintf("mock-record-id-%d", m.lastId)

	m.records = append(m.records, rr)
	return &cloudflare.DNSRecordResponse{}, nil
}

func (m *mockCloudflareApi) UpdateDNSRecord(ctx context.Context, zoneId, recordId string, rr cloudflare.DNSRecord) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	for i, record := range m.records {
		if record.ID == recordId {
			m.records[i].Content = rr.Content
			return nil
		}
	}

	return fmt.Errorf("record with id %s not found", recordId)
}

func (m *mockCloudflareApi) DeleteDNSRecord(ctx context.Context, zoneId, recordId string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	for i, record := range m.records {
		if record.ID == recordId {
			m.records = append(m.records[:i], m.records[i+1:]...)
			return nil
		}
	}

	return fmt.Errorf("record with id %s not found", recordId)
}

func (m *mockCloudflareApi) ZoneIDByName(zoneName string) (string, error) {
	return "mock-zone-id", nil
}

func Test_CloudflareStore(t *testing.T) {
	t.Parallel()

	mockApi := newMockCloudflareApi()
	store := store.NewCloudflareStore(mockApi, "example.com")
	ctx := context.Background()

	key := "key"
	value := "value"

	err := store.Set(ctx, key, value)
	if err != nil {
		t.Fatal(err)
	}

	gotValue, err := store.Get(ctx, key)
	if err != nil {
		t.Fatal(err)
	}

	if gotValue != value {
		t.Fatalf("expected value %s, got %s", value, gotValue)
	}
}

func Test_CloudflareStore_ExistsDuplicate(t *testing.T) {
	mockApi := newMockCloudflareApi()
	store := store.NewCloudflareStore(mockApi, "example.com")
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		_, err := mockApi.CreateDNSRecord(ctx, "mock-zone-id", cloudflare.DNSRecord{
			Type:    "TXT",
			Content: fmt.Sprintf("value-%d", i),
			Name:    "key.example.com",
		})

		if err != nil {
			t.Fatal(err)
		}
	}

	key := "key"
	value := "value"

	err := store.Set(ctx, key, value)
	if err != nil {
		t.Fatal(err)
	}

	gotValue, err := store.Get(ctx, key)
	if err != nil {
		t.Fatal(err)
	}

	if gotValue != value {
		t.Fatalf("expected value %s, got %s", value, gotValue)
	}
}

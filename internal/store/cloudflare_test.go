package store_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/cloudflare/cloudflare-go"
	"github.com/tjjh89017/stunmesh-go/internal/store"
	"github.com/tjjh89017/stunmesh-go/internal/store/store_mock"
	"go.uber.org/mock/gomock"
)

type mockCloudflareStore struct {
	mutex   sync.RWMutex
	lastId  int
	records []cloudflare.DNSRecord
}

var mockData mockCloudflareStore

func listDNSRecords(ctx context.Context, rc *cloudflare.ResourceContainer, params cloudflare.ListDNSRecordsParams) ([]cloudflare.DNSRecord, *cloudflare.ResultInfo, error) {
	mockData.mutex.RLock()
	defer mockData.mutex.RUnlock()

	matchedRecords := []cloudflare.DNSRecord{}

	for _, record := range mockData.records {
		isNameMatched := record.Name == params.Name
		isTypeMatched := record.Type == params.Type

		if isNameMatched && isTypeMatched {
			matchedRecords = append(matchedRecords, record)
		}
	}

	return matchedRecords, &cloudflare.ResultInfo{Count: len(matchedRecords)}, nil
}

func createDNSRecord(ctx context.Context, rc *cloudflare.ResourceContainer, params cloudflare.CreateDNSRecordParams) (cloudflare.DNSRecord, error) {
	mockData.mutex.Lock()
	defer mockData.mutex.Unlock()

	mockData.lastId++
	record := cloudflare.DNSRecord{
		ID:      fmt.Sprintf("mock-record-id-%d", mockData.lastId),
		Type:    params.Type,
		Content: params.Content,
		Name:    params.Name,
	}

	mockData.records = append(mockData.records, record)
	return record, nil
}

func updateDNSRecord(ctx context.Context, rc *cloudflare.ResourceContainer, params cloudflare.UpdateDNSRecordParams) (cloudflare.DNSRecord, error) {
	mockData.mutex.Lock()
	defer mockData.mutex.Unlock()

	for i, record := range mockData.records {
		if record.ID == params.ID {
			mockData.records[i].Content = params.Content
			return mockData.records[i], nil
		}
	}

	return cloudflare.DNSRecord{}, fmt.Errorf("record with id %s not found", params.ID)
}

func deleteDNSRecord(ctx context.Context, rc *cloudflare.ResourceContainer, recordId string) error {
	mockData.mutex.Lock()
	defer mockData.mutex.Unlock()

	for i, record := range mockData.records {
		if record.ID == recordId {
			mockData.records = append(mockData.records[:i], mockData.records[i+1:]...)
			return nil
		}
	}

	return fmt.Errorf("record with id %s not found", recordId)
}

func zoneIDByName(zoneName string) (string, error) {
	return "mock-zone-id", nil
}

func setup(t *testing.T) (context.Context, *store_mock.MockCloudflareApi) {
	t.Helper()

	// init mock
	ctrl := gomock.NewController(t)
	mock := store_mock.NewMockCloudflareApi(ctrl)

	// setup common mock expectation
	mock.EXPECT().ZoneIDByName(gomock.Any()).DoAndReturn(zoneIDByName).AnyTimes()

	// init data
	mockData.mutex.Lock()
	defer mockData.mutex.Unlock()

	mockData.lastId = 0
	mockData.records = []cloudflare.DNSRecord{}
	return context.Background(), mock
}

func Test_CloudflareStore(t *testing.T) {
	t.Parallel()

	ctx, mockApi := setup(t)

	// mock expect for Set() operation
	mockApi.EXPECT().ListDNSRecords(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(listDNSRecords)
	mockApi.EXPECT().CreateDNSRecord(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(createDNSRecord)

	// mock expect for Get() operation
	mockApi.EXPECT().ListDNSRecords(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(listDNSRecords)

	store := store.NewCloudflareStore(mockApi, "example.com")

	const key = "key"
	const value = "value"

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
	const existedRecordCnt = 3

	ctx, mockApi := setup(t)

	// mock expect for Set() operation
	mockApi.EXPECT().ListDNSRecords(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(listDNSRecords)
	mockApi.EXPECT().DeleteDNSRecord(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(deleteDNSRecord).Times(existedRecordCnt - 1)
	mockApi.EXPECT().UpdateDNSRecord(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(updateDNSRecord)

	// mock expect for Get() operation
	mockApi.EXPECT().ListDNSRecords(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(listDNSRecords)

	store := store.NewCloudflareStore(mockApi, "example.com")
	for i := 0; i < existedRecordCnt; i++ {
		_, err := createDNSRecord(ctx, cloudflare.ZoneIdentifier("mock-zone-id"), cloudflare.CreateDNSRecordParams{
			Type:    "TXT",
			Content: fmt.Sprintf("value-%d", i),
			Name:    "key.example.com",
		})

		if err != nil {
			t.Fatal(err)
		}
	}

	const key = "key"
	const value = "value"

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

func Test_CloudflareStore_SetTheSameRecord(t *testing.T) {
	ctx, mockApi := setup(t)

	// mock expect for Set() operation
	mockApi.EXPECT().ListDNSRecords(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(listDNSRecords).Times(2)
	mockApi.EXPECT().CreateDNSRecord(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(createDNSRecord)

	// mock expect for Get() operation
	mockApi.EXPECT().ListDNSRecords(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(listDNSRecords)

	store := store.NewCloudflareStore(mockApi, "example.com")

	const key = "key"
	const value = "value"

	err := store.Set(ctx, key, value)
	if err != nil {
		t.Fatal(err)
	}

	err2 := store.Set(ctx, key, value)
	if err2 != nil {
		t.Fatal(err2)
	}

	gotValue, err := store.Get(ctx, key)
	if err != nil {
		t.Fatal(err)
	}

	if gotValue != value {
		t.Fatalf("expected value %s, got %s", value, gotValue)
	}
}

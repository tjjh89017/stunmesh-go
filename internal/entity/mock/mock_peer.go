// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/tjjh89017/stunmesh-go/internal/entity (interfaces: ConfigPeerProvider,DevicePeerChecker)
//
// Generated by this command:
//
//	mockgen -destination=./mock/mock_peer.go -package=mock_entity . ConfigPeerProvider,DevicePeerChecker
//

// Package mock_entity is a generated GoMock package.
package mock_entity

import (
	context "context"
	reflect "reflect"

	entity "github.com/tjjh89017/stunmesh-go/internal/entity"
	gomock "go.uber.org/mock/gomock"
)

// MockConfigPeerProvider is a mock of ConfigPeerProvider interface.
type MockConfigPeerProvider struct {
	ctrl     *gomock.Controller
	recorder *MockConfigPeerProviderMockRecorder
	isgomock struct{}
}

// MockConfigPeerProviderMockRecorder is the mock recorder for MockConfigPeerProvider.
type MockConfigPeerProviderMockRecorder struct {
	mock *MockConfigPeerProvider
}

// NewMockConfigPeerProvider creates a new mock instance.
func NewMockConfigPeerProvider(ctrl *gomock.Controller) *MockConfigPeerProvider {
	mock := &MockConfigPeerProvider{ctrl: ctrl}
	mock.recorder = &MockConfigPeerProviderMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockConfigPeerProvider) EXPECT() *MockConfigPeerProviderMockRecorder {
	return m.recorder
}

// GetConfigPeers mocks base method.
func (m *MockConfigPeerProvider) GetConfigPeers(ctx context.Context, deviceName string, localPublicKey []byte) ([]*entity.Peer, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetConfigPeers", ctx, deviceName, localPublicKey)
	ret0, _ := ret[0].([]*entity.Peer)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetConfigPeers indicates an expected call of GetConfigPeers.
func (mr *MockConfigPeerProviderMockRecorder) GetConfigPeers(ctx, deviceName, localPublicKey any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetConfigPeers", reflect.TypeOf((*MockConfigPeerProvider)(nil).GetConfigPeers), ctx, deviceName, localPublicKey)
}

// MockDevicePeerChecker is a mock of DevicePeerChecker interface.
type MockDevicePeerChecker struct {
	ctrl     *gomock.Controller
	recorder *MockDevicePeerCheckerMockRecorder
	isgomock struct{}
}

// MockDevicePeerCheckerMockRecorder is the mock recorder for MockDevicePeerChecker.
type MockDevicePeerCheckerMockRecorder struct {
	mock *MockDevicePeerChecker
}

// NewMockDevicePeerChecker creates a new mock instance.
func NewMockDevicePeerChecker(ctrl *gomock.Controller) *MockDevicePeerChecker {
	mock := &MockDevicePeerChecker{ctrl: ctrl}
	mock.recorder = &MockDevicePeerCheckerMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockDevicePeerChecker) EXPECT() *MockDevicePeerCheckerMockRecorder {
	return m.recorder
}

// GetDevicePeerMap mocks base method.
func (m *MockDevicePeerChecker) GetDevicePeerMap(ctx context.Context, deviceName string) (map[string]bool, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetDevicePeerMap", ctx, deviceName)
	ret0, _ := ret[0].(map[string]bool)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetDevicePeerMap indicates an expected call of GetDevicePeerMap.
func (mr *MockDevicePeerCheckerMockRecorder) GetDevicePeerMap(ctx, deviceName any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetDevicePeerMap", reflect.TypeOf((*MockDevicePeerChecker)(nil).GetDevicePeerMap), ctx, deviceName)
}

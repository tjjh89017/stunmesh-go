package entity_test

import (
	"testing"

	"github.com/tjjh89017/stunmesh-go/internal/entity"
)

func Test_PeerId_EndpointKey(t *testing.T) {
	devicePublicKey := []byte{0}
	peerPublicKey := []byte{1}

	peerId := entity.NewPeerId(devicePublicKey, peerPublicKey)
	endpointKey := peerId.EndpointKey()

	expected := "37b7dcf21e0e183a9a86170997df242a84a85ff7"
	if endpointKey != expected {
		t.Errorf("Expected %s, got %s", expected, endpointKey)
	}
}

func Test_PeerId_RemoteEndpointKey(t *testing.T) {
	devicePublicKey := []byte{0}
	peerPublicKey := []byte{1}

	peerId := entity.NewPeerId(devicePublicKey, peerPublicKey)
	remoteEndpointKey := peerId.RemoteEndpointKey()

	expected := "9c8d8e5a31c9802b093c4116dfb0a23a311b8029"
	if remoteEndpointKey != expected {
		t.Errorf("Expected %s, got %s", expected, remoteEndpointKey)
	}
}

func Test_PeerId_Comparable(t *testing.T) {
	pid := entity.NewPeerId([]byte{0}, []byte{1})
	pid2 := entity.NewPeerId([]byte{0}, []byte{1})

	store := make(map[entity.PeerId]int)
	store[pid] = 1
	store[pid2] = 2

	if store[pid] != 2 {
		t.Errorf("Expected 2, got %d", store[pid])
	}
}

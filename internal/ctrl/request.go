package ctrl

import (
	"github.com/tjjh89017/stunmesh-go/internal/entity"
	"github.com/tjjh89017/stunmesh-go/pluginapi"
)

func requestFor(device *entity.Device, peer *entity.Peer, key string) pluginapi.PluginRequest {
	return pluginapi.PluginRequest{
		Device:         string(device.Name()),
		LocalPublicKey: pluginapi.Key(peer.Id().DevicePublicKey()),
		PeerPublicKey:  pluginapi.Key(peer.PublicKey()),
		Key:            key,
	}
}

//go:build freebsd

package ctrl

import (
	"context"
	"encoding/base64"
	"os/exec"
	"strconv"

	"github.com/tjjh89017/stunmesh-go/internal/entity"
)

func (c *EstablishController) ConfigureDevice(ctx context.Context, peer *entity.Peer, res *EndpointDecryptResponse) error {
	remoteEndpoint := res.Host + ":" + strconv.FormatInt(int64(res.Port), 10)
	c.logger.Debug().Str("peer", peer.LocalId()).Str("remote", remoteEndpoint).Msg("configuring device for peer")

	var publicKeyArray [32]byte = peer.PublicKey()
	publicKey := base64.StdEncoding.EncodeToString(publicKeyArray[:])

	// compare the current endpoint with the existing one
	device, err := c.wgCtrl.Device(peer.DeviceName())
	if err != nil {
		c.logger.Error().Err(err).Msg("failed to get device")
		return err
	}
	if device != nil {
		for _, p := range device.Peers {
			if p.PublicKey.String() == publicKey {
				if p.Endpoint.String() == remoteEndpoint {
					c.logger.Debug().Msg("endpoint already configured, skipping")
					return nil
				}
			}
		}
	}

	r, err := exec.Command("wg", "set", peer.DeviceName(), "peer", publicKey, "endpoint", remoteEndpoint).Output()
	c.logger.Debug().Str("output", string(r)).Msg("wg set command executed")
	return err
}

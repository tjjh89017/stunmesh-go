//go:build freebsd

package ctrl

import (
	"context"
	"encoding/base64"
	"os/exec"

	"github.com/tjjh89017/stunmesh-go/internal/entity"
)

func (c *EstablishController) ConfigureDevice(ctx context.Context, peer *entity.Peer, res *EndpointDecryptResponse) error {
	c.logger.Debug().Str("peer", peer.LocalId()).Msg("configuring device for peer")

	var publicKeyArray [32]byte = peer.PublicKey()
	publicKey := base64.StdEncoding.EncodeToString(publicKeyArray[:])
	r, err := exec.Command("wg", "set", peer.DeviceName(), "peer", publicKey, "endpoint", res.Host+":"+string(res.Port)).Output()
	c.logger.Debug().Str("output", string(r)).Msg("wg set command executed")
	return err
}

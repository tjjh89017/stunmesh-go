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
	r, err := exec.Command("wg", "set", peer.DeviceName(), "peer", publicKey, "endpoint", remoteEndpoint).Output()
	c.logger.Debug().Str("output", string(r)).Msg("wg set command executed")
	return err
}

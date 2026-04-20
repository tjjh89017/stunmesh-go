//go:build wgcli

package wg

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
)

type Runner func(ctx context.Context, name string, args ...string) ([]byte, error)

type cliClient struct {
	runner Runner
}

func defaultRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if stderr.Len() > 0 {
			return nil, fmt.Errorf("%s: %w: %s", name, err, strings.TrimSpace(stderr.String()))
		}
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return out, nil
}

func New() (Client, error) {
	return &cliClient{runner: defaultRunner}, nil
}

func (c *cliClient) Device(name string) (*DeviceInfo, error) {
	out, err := c.runner(context.Background(), "wg", "show", name, "dump")
	if err != nil {
		return nil, fmt.Errorf("wg show dump: %w", err)
	}
	return parseDeviceDump(name, out)
}

func parseDeviceDump(name string, output []byte) (*DeviceInfo, error) {
	lines := bytes.Split(output, []byte("\n"))
	if len(lines) == 0 || len(bytes.TrimSpace(lines[0])) == 0 {
		return nil, fmt.Errorf("wg show dump: empty output")
	}

	devFields := strings.Split(string(lines[0]), "\t")
	if len(devFields) < 4 {
		return nil, fmt.Errorf("wg show dump: failed to parse device line")
	}

	priv, err := decodeKey(devFields[0])
	if err != nil {
		return nil, fmt.Errorf("wg show dump: failed to decode private key")
	}
	pub, err := decodeKey(devFields[1])
	if err != nil {
		return nil, fmt.Errorf("wg show dump: failed to decode public key")
	}
	port, err := strconv.Atoi(devFields[2])
	if err != nil {
		return nil, fmt.Errorf("wg show dump: failed to parse listen-port")
	}

	var peerKeys []Key
	for _, raw := range lines[1:] {
		line := bytes.TrimSpace(raw)
		if len(line) == 0 {
			continue
		}
		fields := strings.Split(string(line), "\t")
		if len(fields) < 1 {
			return nil, fmt.Errorf("wg show dump: failed to parse peer line")
		}
		pk, err := decodeKey(fields[0])
		if err != nil {
			return nil, fmt.Errorf("wg show dump: failed to decode peer key")
		}
		peerKeys = append(peerKeys, pk)
	}

	return &DeviceInfo{
		Name:       name,
		ListenPort: port,
		PrivateKey: priv,
		PublicKey:  pub,
		PeerKeys:   peerKeys,
	}, nil
}

func decodeKey(s string) (Key, error) {
	var k Key
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return k, err
	}
	if len(b) != 32 {
		return k, fmt.Errorf("invalid key length %d", len(b))
	}
	copy(k[:], b)
	return k, nil
}

func (c *cliClient) UpdatePeerEndpoint(u PeerEndpointUpdate) error {
	endpoint := net.JoinHostPort(u.Host, strconv.Itoa(u.Port))
	pk := base64.StdEncoding.EncodeToString(u.PublicKey[:])
	_, err := c.runner(context.Background(), "wg", "set", u.DeviceName, "peer", pk, "endpoint", endpoint)
	if err != nil {
		return fmt.Errorf("wg set endpoint: %w", err)
	}
	return nil
}

func (c *cliClient) Close() error {
	return nil
}

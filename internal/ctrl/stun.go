//go:generate mockgen -destination=./mock/mock_stun.go -package=mock_ctrl . StunResolver

package ctrl

import "context"

type StunResolver interface {
	Resolve(ctx context.Context, deviceName string, port uint16, protocol string) (string, int, error)
}

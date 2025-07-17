package ctrl

import "context"

type StunResolver interface {
	Resolve(ctx context.Context, deviceName string, port uint16) (string, int, error)
}

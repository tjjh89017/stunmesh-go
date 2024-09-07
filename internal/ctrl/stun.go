package ctrl

import "context"

type StunResolver interface {
	Resolve(ctx context.Context, port uint16) (string, int, error)
}

// build +darwin
package stun

import "context"

// NOTE: This is a placeholder implementation for darwin.
type Stun struct {
}

func New(ctx context.Context, port uint16) (*Stun, error) {
	return &Stun{}, nil
}

func (s *Stun) Stop() error {
	return nil
}

func (s *Stun) Start(ctx context.Context) {
}

func (s *Stun) Connect(ctx context.Context, address string) (_ string, _ int, err error) {
	return "", 0, nil
}

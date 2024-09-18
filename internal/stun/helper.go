package stun

import (
	"context"
	"errors"

	"github.com/pion/stun"
	"github.com/rs/zerolog"
)

var (
	ErrResponseMessage = errors.New("error reading from response message channel")
	ErrTimeout         = errors.New("timed out waiting for response")
)

const BindingPacketHeaderSize = 8

var (
	StunTimeout = 5
)

func Parse(ctx context.Context, msg *stun.Message) *stun.XORMappedAddress {
	logger := zerolog.Ctx(ctx)

	mappedAddr := &stun.MappedAddress{}
	xorAddr := &stun.XORMappedAddress{}
	otherAddr := &stun.OtherAddress{}
	software := &stun.Software{}

	if xorAddr.GetFrom(msg) != nil {
		xorAddr = nil
	}
	if otherAddr.GetFrom(msg) != nil {
		otherAddr = nil
	}
	if mappedAddr.GetFrom(msg) != nil {
		mappedAddr = nil
	}
	if software.GetFrom(msg) != nil {
		software = nil
	}

	logger.Debug().Any("MAPPED-ADDRESS", mappedAddr).Any("XOR-MAPPED-ADDRESS", xorAddr).Any("OTHER-ADDRESS", otherAddr).Any("SOFTWARE", software).Msgf("%v", msg)
	return xorAddr
}

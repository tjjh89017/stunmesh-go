package stun

import (
	"errors"
	"log"

	"github.com/pion/stun"
)

var (
	ErrResponseMessage = errors.New("error reading from response message channel")
	ErrTimeout         = errors.New("timed out waiting for response")
)

const BindingPacketHeaderSize = 8

var (
	StunTimeout = 5
)

func Parse(msg *stun.Message) *stun.XORMappedAddress {
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
	log.Printf("%v\n", msg)
	log.Printf("\tMAPPED-ADDRESS:     %v\n", mappedAddr)
	log.Printf("\tXOR-MAPPED-ADDRESS: %v\n", xorAddr)
	log.Printf("\tOTHER-ADDRESS:      %v\n", otherAddr)
	log.Printf("\tSOFTWARE:           %v\n", software)

	return xorAddr
}

package main

import (
	"encoding/binary"
	"log"
	"net"
	"time"

	"github.com/pion/stun"
	"golang.org/x/net/bpf"
)

var (
	StunTimeout = 5
)

type UDPHeader struct {
	SrcPort  uint16
	DstPort  uint16
	Length   uint16
	Checksum uint16
}

func (s *Session) roundTrip(msg *stun.Message, addr *net.UDPAddr) (*stun.Message, error) {
	_ = msg.NewTransactionID()
	log.Printf("Send to %v: (%v bytes)\n", addr, msg.Length)

	send_udp := &UDPHeader{
		SrcPort:  s.LocalPort(),
		DstPort:  uint16(addr.Port),
		Length:   uint16(8 + len(msg.Raw)),
		Checksum: 0,
	}

	buf := make([]byte, 8)
	binary.BigEndian.PutUint16(buf[0:], send_udp.SrcPort)
	binary.BigEndian.PutUint16(buf[2:], send_udp.DstPort)
	binary.BigEndian.PutUint16(buf[4:], send_udp.Length)
	binary.BigEndian.PutUint16(buf[6:], send_udp.Checksum)

	if _, err := s.conn.WriteTo(append(buf, msg.Raw...), nil, addr); err != nil {
		log.Panic(err)
		return nil, err
	}

	// wait for respone
	select {
	case m, ok := <-s.messageChan:
		if !ok {
			return nil, ErrResponseMessage
		}
		return m, nil
	case <-time.After(time.Duration(StunTimeout) * time.Second):
		log.Printf("time out")
		return nil, ErrTimeout
	}
}

func parse(msg *stun.Message) (ret struct {
	xorAddr    *stun.XORMappedAddress
	otherAddr  *stun.OtherAddress
	mappedAddr *stun.MappedAddress
	software   *stun.Software
}) {
	ret.mappedAddr = &stun.MappedAddress{}
	ret.xorAddr = &stun.XORMappedAddress{}
	ret.otherAddr = &stun.OtherAddress{}
	ret.software = &stun.Software{}
	if ret.xorAddr.GetFrom(msg) != nil {
		ret.xorAddr = nil
	}
	if ret.otherAddr.GetFrom(msg) != nil {
		ret.otherAddr = nil
	}
	if ret.mappedAddr.GetFrom(msg) != nil {
		ret.mappedAddr = nil
	}
	if ret.software.GetFrom(msg) != nil {
		ret.software = nil
	}
	log.Printf("%v\n", msg)
	log.Printf("\tMAPPED-ADDRESS:     %v\n", ret.mappedAddr)
	log.Printf("\tXOR-MAPPED-ADDRESS: %v\n", ret.xorAddr)
	log.Printf("\tOTHER-ADDRESS:      %v\n", ret.otherAddr)
	log.Printf("\tSOFTWARE:           %v\n", ret.software)

	return ret
}

func stunBpfFilter(port uint16) ([]bpf.RawInstruction, error) {
	// if possible make some magic here to determine STUN packet
	const (
		ipOff              = 0
		udpOff             = ipOff + 5*4
		payloadOff         = udpOff + 2*4
		stunMagicCookieOff = payloadOff + 4

		stunMagicCookie = 0x2112A442
	)
	r, e := bpf.Assemble([]bpf.Instruction{
		bpf.LoadAbsolute{
			// A = dst port
			Off:  udpOff + 2,
			Size: 2,
		},
		bpf.JumpIf{
			// if A == `port`
			Cond:      bpf.JumpEqual,
			Val:       uint32(port),
			SkipFalse: 3,
		},
		bpf.LoadAbsolute{
			// A = stun magic part
			Off:  stunMagicCookieOff,
			Size: 4,
		},
		bpf.JumpIf{
			// if A == stun magic value
			Cond:      bpf.JumpEqual,
			Val:       stunMagicCookie,
			SkipFalse: 1,
		},
		// we need
		bpf.RetConstant{
			Val: 262144,
		},
		// port and stun are not we need
		bpf.RetConstant{
			Val: 0,
		},
	})
	if e != nil {
		log.Panic(e)
	}
	return r, e
}

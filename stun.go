package main

import (
	"encoding/binary"
	"log"
	"net"
	"time"

	"github.com/pion/stun"
	"golang.org/x/net/bpf"
	"golang.org/x/net/ipv4"
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

type STUNSession struct {
	conn        *ipv4.PacketConn
	innerConn   net.PacketConn
	LocalAddr   net.Addr
	LocalPort   uint16
	RemoteAddr  *net.UDPAddr
	OtherAddr   *net.UDPAddr
	messageChan chan *stun.Message
}

func (c *STUNSession) Close() error {
	return c.conn.Close()
}

func (c *STUNSession) roundTrip(msg *stun.Message, addr net.Addr) (*stun.Message, error) {
	_ = msg.NewTransactionID()
	log.Printf("Send to %v: (%v bytes)\n", addr, msg.Length)

	send_udp := &UDPHeader{
		SrcPort:  c.LocalPort,
		DstPort:  uint16(c.RemoteAddr.Port),
		Length:   uint16(8 + len(msg.Raw)),
		Checksum: 0,
	}

	buf := make([]byte, 8)
	binary.BigEndian.PutUint16(buf[0:], send_udp.SrcPort)
	binary.BigEndian.PutUint16(buf[2:], send_udp.DstPort)
	binary.BigEndian.PutUint16(buf[4:], send_udp.Length)
	binary.BigEndian.PutUint16(buf[6:], send_udp.Checksum)

	if _, err := c.conn.WriteTo(append(buf, msg.Raw...), nil, addr); err != nil {
		log.Panic(err)
		return nil, err
	}

	// wait for respone
	select {
	case m, ok := <-c.messageChan:
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

func connect(port uint16, addrStr string) (*STUNSession, error) {
	log.Printf("connecting to STUN server: %s\n", addrStr)
	addr, err := net.ResolveUDPAddr("udp4", addrStr)
	if err != nil {
		log.Panic(err)
		return nil, err
	}

	c, err := net.ListenPacket("ip4:17", "0.0.0.0")
	if err != nil {
		log.Panic(err)
		return nil, err
	}

	p := ipv4.NewPacketConn(c)
	// set port here
	bpf_filter, err := stun_bpf_filter(port)
	if err != nil {
		log.Panic(err)
	}

	err = p.SetBPF(bpf_filter)
	if err != nil {
		log.Panic(err)
	}

	mChan := listen(p)

	return &STUNSession{
		conn:        p,
		innerConn:   c,
		LocalAddr:   p.LocalAddr(),
		LocalPort:   port,
		RemoteAddr:  addr,
		messageChan: mChan,
	}, nil

}

func listen(conn *ipv4.PacketConn) (messages chan *stun.Message) {
	messages = make(chan *stun.Message)
	go func() {
		for {
			buf := make([]byte, 1500)
			n, _, addr, err := conn.ReadFrom(buf)
			if err != nil {
				close(messages)
				return
			}
			log.Printf("Response from %v: (%v bytes)\n", addr, n)
			// cut UDP header, cut postfix
			buf = buf[8:n]

			m := new(stun.Message)
			m.Raw = buf
			err = m.Decode()
			if err != nil {
				log.Printf("Error decoding message: %v\n", err)
				close(messages)
				return
			}
			messages <- m
		}
	}()
	return
}

func stun_bpf_filter(port uint16) ([]bpf.RawInstruction, error) {
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

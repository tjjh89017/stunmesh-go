package session

import (
	"log"
	"net"

	"github.com/pion/stun"
	"golang.org/x/net/ipv4"
)

type Session struct {
	conn        *ipv4.PacketConn
	messageChan chan *stun.Message
}

func New(localPort uint16) (*Session, error) {
	return &Session{
		messageChan: make(chan *stun.Message),
	}, nil

}

func (s *Session) Wait(stunAddr string, port uint16) (*stun.Message, error) {
	c, err := net.ListenPacket("ip4:17", "0.0.0.0")
	if err != nil {
		return nil, err
	}

	s.conn = ipv4.NewPacketConn(c)
	defer s.conn.Close()

	bpfFilter, err := stunBpfFilter(port)
	if err != nil {
		return nil, err
	}

	err = s.conn.SetBPF(bpfFilter)
	if err != nil {
		return nil, err
	}
	go s.listen()

	request, err := stun.Build(stun.TransactionID, stun.BindingRequest)
	if err != nil {
		return nil, err
	}

	log.Printf("connecting to STUN server: %s\n", stunAddr)
	addr, err := net.ResolveUDPAddr("udp4", stunAddr)
	if err != nil {
		return nil, err
	}

	resData, err := s.RoundTrip(port, request, addr)
	if err != nil {
		return nil, err
	}

	return resData, nil
}

func (s *Session) listen() {
	for {
		buf := make([]byte, 1500)
		n, _, addr, err := s.conn.ReadFrom(buf)
		if err != nil {
			close(s.messageChan)
			return
		}

		log.Printf("Response from %v: (%v bytes)\n", addr, n)
		// cut UDP header, cut postfix
		buf = buf[8:n]

		m := &stun.Message{
			Raw: buf,
		}

		if err := m.Decode(); err != nil {
			log.Printf("Error decoding message: %v\n", err)
			close(s.messageChan)
			return
		}

		s.messageChan <- m
	}
}

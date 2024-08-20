package main

import (
	"log"
	"net"

	"github.com/pion/stun"
	"golang.org/x/net/ipv4"
)

type Session struct {
	conn        *ipv4.PacketConn
	localPort   uint16
	messageChan chan *stun.Message
}

func NewSession(localPort uint16) (*Session, error) {
	return &Session{
		localPort:   localPort,
		messageChan: make(chan *stun.Message),
	}, nil

}

func (s *Session) Close() error {
	return s.conn.Close()
}

func (s *Session) Start() error {
	c, err := net.ListenPacket("ip4:17", "0.0.0.0")
	if err != nil {
		return err
	}

	s.conn = ipv4.NewPacketConn(c)
	// set port here
	bpfFilter, err := stunBpfFilter(s.localPort)
	if err != nil {
		return err
	}

	err = s.conn.SetBPF(bpfFilter)
	if err != nil {
		return err
	}

	go s.listen()

	return nil
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

func (s *Session) LocalPort() uint16 {
	return s.localPort
}

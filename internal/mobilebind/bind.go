//go:build mobile

package mobilebind

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"sync"
	"syscall"

	wgconn "golang.zx2c4.com/wireguard/conn"
)

// Protector excludes a socket from the tunnel (VpnService.protect on
// Android). A nil Protector is a no-op, which keeps the bind testable off
// device.
type Protector interface {
	Protect(fd int32) bool
}

// Bind is a conn.Bind that owns the outer UDP sockets and demuxes the
// receive path: STUN-shaped packets go to the transaction registry, all
// other packets go to wireguard-go (which drops what it does not recognize).
// STUN discovery and hole punching therefore share the WG socket, so the
// reflexive address STUN sees is the same mapping WG traffic uses.
//
// SetMark is a no-op: SO_MARK needs CAP_NET_ADMIN, which apps do not have.
type Bind struct {
	mu        sync.Mutex
	protector Protector
	registry  *TxnRegistry

	conn4 *net.UDPConn
	conn6 *net.UDPConn
	open  bool
}

var _ wgconn.Bind = (*Bind)(nil)

func New(protector Protector) *Bind {
	return &Bind{
		protector: protector,
		registry:  NewTxnRegistry(),
	}
}

// Registry exposes the transaction registry to the STUN client.
func (b *Bind) Registry() *TxnRegistry { return b.registry }

func (b *Bind) Open(port uint16) ([]wgconn.ReceiveFunc, uint16, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.open {
		return nil, 0, wgconn.ErrBindAlreadyOpen
	}

	conn4, err := b.listen("udp4", port)
	if err != nil {
		return nil, 0, fmt.Errorf("listen udp4: %w", err)
	}
	actualPort := uint16(conn4.LocalAddr().(*net.UDPAddr).Port)

	// The v6 socket shares the port so both families publish one mapping.
	conn6, err := b.listen("udp6", actualPort)
	if err != nil {
		conn6 = nil // v6 is best-effort; v4-only hosts still work
	}

	b.conn4 = conn4
	b.conn6 = conn6
	b.open = true

	fns := []wgconn.ReceiveFunc{b.receiveFunc(conn4)}
	if conn6 != nil {
		fns = append(fns, b.receiveFunc(conn6))
	}
	return fns, actualPort, nil
}

func (b *Bind) listen(network string, port uint16) (*net.UDPConn, error) {
	lc := net.ListenConfig{
		Control: func(_, _ string, c syscall.RawConn) error {
			if b.protector == nil {
				return nil
			}
			var protectErr error
			err := c.Control(func(fd uintptr) {
				if !b.protector.Protect(int32(fd)) {
					protectErr = errors.New("socket protect failed")
				}
			})
			if err != nil {
				return err
			}
			return protectErr
		},
	}
	pc, err := lc.ListenPacket(context.TODO(), network, ":"+strconv.Itoa(int(port)))
	if err != nil {
		return nil, err
	}
	return pc.(*net.UDPConn), nil
}

func (b *Bind) receiveFunc(conn *net.UDPConn) wgconn.ReceiveFunc {
	return func(bufs [][]byte, sizes []int, eps []wgconn.Endpoint) (int, error) {
		for {
			n, addr, err := conn.ReadFromUDPAddrPort(bufs[0])
			if err != nil {
				return 0, err
			}
			pkt := bufs[0][:n]
			if IsSTUN(pkt) {
				// Demuxed out of the WG path; unknown transactions drop.
				b.registry.Dispatch(pkt)
				continue
			}
			sizes[0] = n
			eps[0] = &wgconn.StdNetEndpoint{AddrPort: addr}
			return 1, nil
		}
	}
}

func (b *Bind) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	var err4, err6 error
	if b.conn4 != nil {
		err4 = b.conn4.Close()
		b.conn4 = nil
	}
	if b.conn6 != nil {
		err6 = b.conn6.Close()
		b.conn6 = nil
	}
	b.open = false
	return errors.Join(err4, err6)
}

// SetMark is a no-op: SO_MARK requires CAP_NET_ADMIN.
func (b *Bind) SetMark(_ uint32) error { return nil }

func (b *Bind) Send(bufs [][]byte, ep wgconn.Endpoint) error {
	end, ok := ep.(*wgconn.StdNetEndpoint)
	if !ok {
		return wgconn.ErrWrongEndpointType
	}
	conn := b.connFor(end.Addr())
	if conn == nil {
		return net.ErrClosed
	}
	for _, buf := range bufs {
		if _, err := conn.WriteToUDPAddrPort(buf, end.AddrPort); err != nil {
			return err
		}
	}
	return nil
}

// SendTo transmits one STUNMESH packet (STUN request or hole punch) from the
// shared socket outside the WG path.
func (b *Bind) SendTo(addr netip.AddrPort, payload []byte) error {
	conn := b.connFor(addr.Addr())
	if conn == nil {
		return net.ErrClosed
	}
	_, err := conn.WriteToUDPAddrPort(payload, addr)
	return err
}

func (b *Bind) connFor(addr netip.Addr) *net.UDPConn {
	b.mu.Lock()
	defer b.mu.Unlock()
	if addr.Is4() || addr.Is4In6() {
		return b.conn4
	}
	if b.conn6 != nil {
		return b.conn6
	}
	return b.conn4
}

func (b *Bind) ParseEndpoint(s string) (wgconn.Endpoint, error) {
	ap, err := netip.ParseAddrPort(s)
	if err != nil {
		return nil, err
	}
	return &wgconn.StdNetEndpoint{AddrPort: ap}, nil
}

// BatchSize is 1: one packet per syscall. UDP GSO/GRO batching is a later
// optimization and changes no semantics.
func (b *Bind) BatchSize() int { return 1 }

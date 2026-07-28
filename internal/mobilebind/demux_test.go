//go:build mobile

package mobilebind

import (
	"encoding/binary"
	"testing"
)

func stunPacket(t *testing.T, msgType uint16, txn TxnID, attrs []byte) []byte {
	t.Helper()
	msg := make([]byte, stunHeaderSize+len(attrs))
	binary.BigEndian.PutUint16(msg[0:2], msgType)
	binary.BigEndian.PutUint16(msg[2:4], uint16(len(attrs)))
	binary.BigEndian.PutUint32(msg[4:8], stunMagicCookie)
	copy(msg[8:20], txn[:])
	copy(msg[stunHeaderSize:], attrs)
	return msg
}

func TestIsSTUN(t *testing.T) {
	txn := TxnID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}

	if !IsSTUN(stunPacket(t, stunBindingRequest, txn, nil)) {
		t.Error("binding request not detected as STUN")
	}
	if !IsSTUN(stunPacket(t, stunBindingSuccess, txn, make([]byte, 12))) {
		t.Error("binding success with attributes not detected as STUN")
	}

	// A WireGuard handshake initiation: type byte 1, reserved zeros, then
	// sender index — no magic cookie at offset 4.
	wg := make([]byte, 148)
	wg[0] = 1
	if IsSTUN(wg) {
		t.Error("WireGuard handshake initiation misdetected as STUN")
	}

	if IsSTUN(nil) || IsSTUN(make([]byte, 19)) {
		t.Error("short packet misdetected as STUN")
	}

	// First two bits must be zero.
	bad := stunPacket(t, stunBindingRequest, txn, nil)
	bad[0] |= 0xC0
	if IsSTUN(bad) {
		t.Error("packet with non-zero leading bits misdetected as STUN")
	}

	// Length field inconsistent with the packet.
	short := stunPacket(t, stunBindingRequest, txn, nil)
	binary.BigEndian.PutUint16(short[2:4], 8)
	if IsSTUN(short) {
		t.Error("packet with overlong length field misdetected as STUN")
	}
}

func TestTxnRegistryDispatch(t *testing.T) {
	r := NewTxnRegistry()
	txn := TxnID{9, 9, 9}
	ch := r.Register(txn)

	pkt := stunPacket(t, stunBindingSuccess, txn, nil)
	if !r.Dispatch(pkt) {
		t.Fatal("dispatch to registered txn failed")
	}
	select {
	case got := <-ch:
		if string(got) != string(pkt) {
			t.Error("dispatched packet differs from sent packet")
		}
	default:
		t.Fatal("no packet on channel")
	}

	// The dispatched packet must be a copy.
	pkt2 := stunPacket(t, stunBindingSuccess, txn, nil)
	r.Dispatch(pkt2)
	pkt2[0] = 0xFF
	got := <-ch
	if got[0] == 0xFF {
		t.Error("dispatch did not copy the packet")
	}

	r.Unregister(txn)
	if r.Dispatch(stunPacket(t, stunBindingSuccess, txn, nil)) {
		t.Error("dispatch succeeded after unregister")
	}

	other := TxnID{1}
	if r.Dispatch(stunPacket(t, stunBindingSuccess, other, nil)) {
		t.Error("dispatch succeeded for unknown txn")
	}
}

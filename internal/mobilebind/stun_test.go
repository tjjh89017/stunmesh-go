package mobilebind

import (
	"encoding/binary"
	"net/netip"
	"testing"
)

func xorMappedV4(t *testing.T, addr netip.AddrPort) []byte {
	t.Helper()
	v := make([]byte, 12)
	binary.BigEndian.PutUint16(v[0:2], attrXorMappedAddress)
	binary.BigEndian.PutUint16(v[2:4], 8)
	v[5] = 0x01
	binary.BigEndian.PutUint16(v[6:8], addr.Port()^uint16(stunMagicCookie>>16))
	raw := addr.Addr().As4()
	var cookie [4]byte
	binary.BigEndian.PutUint32(cookie[:], stunMagicCookie)
	for i := range raw {
		v[8+i] = raw[i] ^ cookie[i]
	}
	return v
}

func TestBuildBindingRequest(t *testing.T) {
	req, txn, err := buildBindingRequest()
	if err != nil {
		t.Fatal(err)
	}
	if len(req) != stunHeaderSize {
		t.Fatalf("request length %d, want %d", len(req), stunHeaderSize)
	}
	if !IsSTUN(req) {
		t.Error("request not STUN-shaped")
	}
	if TxnIDOf(req) != txn {
		t.Error("txn id mismatch")
	}
	if binary.BigEndian.Uint16(req[0:2]) != stunBindingRequest {
		t.Error("wrong message type")
	}
}

func TestParseBindingResponseXorV4(t *testing.T) {
	want := netip.MustParseAddrPort("203.0.113.7:54321")
	txn := TxnID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
	resp := stunPacket(t, stunBindingSuccess, txn, xorMappedV4(t, want))

	got, err := parseBindingResponse(resp, txn)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseBindingResponseXorV6(t *testing.T) {
	want := netip.MustParseAddrPort("[2001:db8::42]:4242")
	txn := TxnID{12, 11, 10, 9, 8, 7, 6, 5, 4, 3, 2, 1}

	v := make([]byte, 24)
	binary.BigEndian.PutUint16(v[0:2], attrXorMappedAddress)
	binary.BigEndian.PutUint16(v[2:4], 20)
	v[5] = 0x02
	binary.BigEndian.PutUint16(v[6:8], want.Port()^uint16(stunMagicCookie>>16))
	raw := want.Addr().As16()
	var mask [16]byte
	binary.BigEndian.PutUint32(mask[0:4], stunMagicCookie)
	copy(mask[4:], txn[:])
	for i := range raw {
		v[8+i] = raw[i] ^ mask[i]
	}
	resp := stunPacket(t, stunBindingSuccess, txn, v)

	got, err := parseBindingResponse(resp, txn)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseBindingResponseRejects(t *testing.T) {
	txn := TxnID{1}
	good := stunPacket(t, stunBindingSuccess, txn, xorMappedV4(t, netip.MustParseAddrPort("192.0.2.1:1234")))

	// Wrong transaction id.
	if _, err := parseBindingResponse(good, TxnID{2}); err == nil {
		t.Error("accepted response with wrong txn id")
	}

	// Error response type.
	bad := stunPacket(t, 0x0111, txn, nil)
	if _, err := parseBindingResponse(bad, txn); err == nil {
		t.Error("accepted error response")
	}

	// No address attribute.
	empty := stunPacket(t, stunBindingSuccess, txn, nil)
	if _, err := parseBindingResponse(empty, txn); err == nil {
		t.Error("accepted response without mapped address")
	}
}

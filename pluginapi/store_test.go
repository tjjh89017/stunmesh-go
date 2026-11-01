package pluginapi

import (
	"testing"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

func TestKeyStringMatchesWireGuard(t *testing.T) {
	priv, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	pub := priv.PublicKey()

	if got, want := Key(pub).String(), pub.String(); got != want {
		t.Errorf("Key.String() = %q, want %q", got, want)
	}
	if got, want := (Key{}).String(), (wgtypes.Key{}).String(); got != want {
		t.Errorf("zero Key.String() = %q, want %q", got, want)
	}
}

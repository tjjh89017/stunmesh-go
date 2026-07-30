//go:build mobile && (linux || android)

package mobile

import (
	"reflect"
	"testing"
)

// SetDNSServers takes the list as one gomobile-safe string; entries are
// comma-separated with optional whitespace, and empties are dropped so a
// trailing comma or a plain "" cannot smuggle in an unusable entry.
func TestSetDNSServersParsing(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"plain", "192.0.2.1,192.0.2.2", []string{"192.0.2.1", "192.0.2.2"}},
		{"spaces and trailing comma", " 192.0.2.1 , 2001:db8::1 ,", []string{"192.0.2.1", "2001:db8::1"}},
		{"with ports", "192.0.2.1:53,[2001:db8::1]:53", []string{"192.0.2.1:53", "[2001:db8::1]:53"}},
		{"hostnames dropped", "dns.example,192.0.2.1", []string{"192.0.2.1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n := &Node{}
			n.SetDNSServers(tc.in)
			if got := n.pluginDNSServers(); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("pluginDNSServers() = %v, want %v", got, tc.want)
			}
		})
	}
}

// Before the app supplies a list -- and again after it clears one -- lookups
// fall back to the public resolvers, so plugins work out of the box.
func TestPluginDNSServersFallback(t *testing.T) {
	n := &Node{}
	if got := n.pluginDNSServers(); !reflect.DeepEqual(got, defaultPluginDNSServers) {
		t.Errorf("zero-value node: pluginDNSServers() = %v, want fallback %v", got, defaultPluginDNSServers)
	}

	n.SetDNSServers("192.0.2.1")
	n.SetDNSServers("  ,, ")
	if got := n.pluginDNSServers(); !reflect.DeepEqual(got, defaultPluginDNSServers) {
		t.Errorf("after clearing: pluginDNSServers() = %v, want fallback %v", got, defaultPluginDNSServers)
	}

	n.SetDNSServers("dns.example,other.example")
	if got := n.pluginDNSServers(); !reflect.DeepEqual(got, defaultPluginDNSServers) {
		t.Errorf("all-invalid list: pluginDNSServers() = %v, want fallback %v", got, defaultPluginDNSServers)
	}
}

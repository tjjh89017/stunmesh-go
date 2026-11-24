package main

import (
	"net/netip"
	"testing"
)

func mp(server, mapped string) mapping {
	return mapping{netip.MustParseAddrPort(server), netip.MustParseAddrPort(mapped)}
}

func TestClassify(t *testing.T) {
	base := result{unknown, unknown, unknown, "4000", "yes", unknown}
	tests := []struct {
		name     string
		pairs    []mapping
		previous string
		mapping  string
		publicIP string
		changed  string
	}{
		{"none", nil, "", unknown, unknown, unknown},
		{"one server", []mapping{mp("192.0.2.1:3478", "203.0.113.1:1000")}, "", unknown, "203.0.113.1", unknown},
		{"independent", []mapping{
			mp("192.0.2.1:3478", "203.0.113.1:1000"),
			mp("192.0.2.2:3478", "203.0.113.1:1000"),
		}, "", "independent", "203.0.113.1", unknown},
		{"dependent port", []mapping{
			mp("192.0.2.1:3478", "203.0.113.1:1000"),
			mp("192.0.2.2:3478", "203.0.113.1:1001"),
		}, "", "dependent", "203.0.113.1", unknown},
		{"dependent ip", []mapping{
			mp("192.0.2.1:3478", "203.0.113.1:1000"),
			mp("192.0.2.2:3478", "203.0.113.2:1000"),
		}, "", "dependent", "203.0.113.1,203.0.113.2", unknown},
		{"unchanged", []mapping{
			mp("192.0.2.1:3478", "203.0.113.1:1000"),
			mp("192.0.2.2:3478", "203.0.113.1:1000"),
		}, "192.0.2.1:3478>203.0.113.1:1000,192.0.2.2:3478>203.0.113.1:1000", "independent", "203.0.113.1", "no"},
		{"changed", []mapping{
			mp("192.0.2.1:3478", "203.0.113.1:1000"),
			mp("192.0.2.2:3478", "203.0.113.1:1000"),
		}, "192.0.2.1:3478>203.0.113.1:2000", "independent", "203.0.113.1", "yes"},
		{"no common server", []mapping{
			mp("192.0.2.1:3478", "203.0.113.1:1000"),
		}, "192.0.2.9:3478>203.0.113.1:1000", unknown, "203.0.113.1", unknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := classify(base, tt.pairs, tt.previous)
			if r.mapping != tt.mapping || r.publicIP != tt.publicIP || r.changed != tt.changed {
				t.Errorf("got mapping=%s public_ip=%s changed=%s, want %s %s %s",
					r.mapping, r.publicIP, r.changed, tt.mapping, tt.publicIP, tt.changed)
			}
		})
	}
}

func TestClassifyNeedsSamePort(t *testing.T) {
	r := result{unknown, unknown, unknown, "4001", "no", unknown}
	r = classify(r, []mapping{mp("192.0.2.1:3478", "203.0.113.1:1000")}, "192.0.2.1:3478>203.0.113.1:2000")
	if r.changed != unknown {
		t.Errorf("changed=%s with a different local port, want unknown", r.changed)
	}
}

func TestSignalContains(t *testing.T) {
	body := []byte("{\"data\":\"cm9sZT1hbmNob3IgbmF0PWRlcGVuZGVudA==\"}\nnot json\n{\"data\":\"!!\"}\n")
	if !signalContains(body, "role=anchor nat=dependent") {
		t.Error("stored value not found")
	}
	if signalContains(body, "role=subject nat=dependent") {
		t.Error("value found that was not stored")
	}
}

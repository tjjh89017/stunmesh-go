package routeprobe

import "testing"

func TestUnicastIfNetworkOrder(t *testing.T) {
	tests := []struct {
		name  string
		index uint32
		want  uint32
	}{
		{"zero", 0, 0},
		{"one", 1, 0x01000000},
		{"typical index", 12, 0x0c000000},
		{"already byte swapped", 0x01000000, 1},
		{"all bytes distinct", 0x01020304, 0x04030201},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := UnicastIfNetworkOrder(tt.index)
			if got != tt.want {
				t.Errorf("UnicastIfNetworkOrder(%#x) = %#x, want %#x", tt.index, got, tt.want)
			}
			if roundTrip := UnicastIfNetworkOrder(got); roundTrip != tt.index {
				t.Errorf("UnicastIfNetworkOrder is not self-inverse: got %#x, want %#x", roundTrip, tt.index)
			}
		})
	}
}

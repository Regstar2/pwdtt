package backend

import "testing"

func TestTunnelTrafficConfirmed(t *testing.T) {
	tests := []struct {
		name      string
		active    int32
		bytesUp   int64
		bytesDown int64
		want      bool
	}{
		{name: "bidirectional active tunnel", active: 1, bytesUp: 128, bytesDown: 64, want: true},
		{name: "no active dtls", active: 0, bytesUp: 128, bytesDown: 64, want: false},
		{name: "upload only", active: 1, bytesUp: 128, bytesDown: 0, want: false},
		{name: "download only", active: 1, bytesUp: 0, bytesDown: 64, want: false},
		{name: "no traffic", active: 1, bytesUp: 0, bytesDown: 0, want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tunnelTrafficConfirmed(tc.active, tc.bytesUp, tc.bytesDown); got != tc.want {
				t.Fatalf("tunnelTrafficConfirmed() = %v, want %v", got, tc.want)
			}
		})
	}
}

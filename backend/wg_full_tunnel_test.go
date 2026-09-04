package backend

import "testing"

func TestIsIPv4FullTunnel(t *testing.T) {
	tests := []struct {
		name       string
		allowedIPs []string
		want       bool
	}{
		{name: "default route", allowedIPs: []string{"0.0.0.0/0"}, want: true},
		{name: "default route with IPv6", allowedIPs: []string{"0.0.0.0/0", "::/0"}, want: true},
		{name: "split tunnel", allowedIPs: []string{"10.0.0.0/8", "192.168.0.0/16"}, want: false},
		{name: "IPv6 only", allowedIPs: []string{"::/0"}, want: false},
		{name: "empty", allowedIPs: nil, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isIPv4FullTunnel(tt.allowedIPs); got != tt.want {
				t.Fatalf("isIPv4FullTunnel(%v) = %v, want %v", tt.allowedIPs, got, tt.want)
			}
		})
	}
}

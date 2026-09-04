package backend

import "testing"

func TestLatencyHost(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"84.32.25.100:56000", "84.32.25.100"},
		{"vpn.example.org:56000", "vpn.example.org"},
		{"[2001:db8::1]:56000", "2001:db8::1"},
		{"2001:db8::1", "2001:db8::1"},
		{"  vpn.example.org:56000  ", "vpn.example.org"},
		{"", ""},
	}
	for _, tc := range tests {
		if got := latencyHost(tc.in); got != tc.want {
			t.Fatalf("latencyHost(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestLatencyPingArgsTargetLast(t *testing.T) {
	const host = "example.org"
	args := latencyPingArgs(host)
	if len(args) < 2 {
		t.Fatalf("latencyPingArgs returned too few arguments: %v", args)
	}
	if args[len(args)-1] != host {
		t.Fatalf("last argument = %q, want %q", args[len(args)-1], host)
	}
}

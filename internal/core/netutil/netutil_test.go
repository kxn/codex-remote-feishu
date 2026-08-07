package netutil

import (
	"net/http"
	"testing"
)

func TestIsLoopbackHost(t *testing.T) {
	cases := []struct {
		host string
		want bool
	}{
		{"localhost", true},
		{"LOCALHOST", true},
		{" localhost ", true},
		{"127.0.0.1", true},
		{"127.0.0.2", true},
		{"::1", true},
		{"[::1]", true},
		{"[::ffff:127.0.0.1]", true},
		{"192.168.1.1", false},
		{"example.com", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := IsLoopbackHost(tc.host); got != tc.want {
			t.Fatalf("IsLoopbackHost(%q) = %v, want %v", tc.host, got, tc.want)
		}
	}
}

func TestIsLoopbackAddress(t *testing.T) {
	cases := []struct {
		addr string
		want bool
	}{
		{"127.0.0.1:8080", true},
		{"localhost:9500", true},
		{"[::1]:9500", true},
		{"192.168.1.1:8080", false},
		{"127.0.0.1", true},
		{"", false},
	}
	for _, tc := range cases {
		if got := IsLoopbackAddress(tc.addr); got != tc.want {
			t.Fatalf("IsLoopbackAddress(%q) = %v, want %v", tc.addr, got, tc.want)
		}
	}
}

func TestIsLoopbackRequest(t *testing.T) {
	if IsLoopbackRequest(nil) {
		t.Fatal("IsLoopbackRequest(nil) = true, want false")
	}
	req := &http.Request{RemoteAddr: "127.0.0.1:1234"}
	if !IsLoopbackRequest(req) {
		t.Fatal("loopback request not detected")
	}
	req = &http.Request{RemoteAddr: "10.0.0.5:1234"}
	if IsLoopbackRequest(req) {
		t.Fatal("non-loopback request detected as loopback")
	}
}

func TestIsLoopbackURL(t *testing.T) {
	cases := []struct {
		raw  string
		want bool
	}{
		{"ws://127.0.0.1:9500/ws/agent", true},
		{"http://localhost:9501/admin", true},
		{"ws://[::1]:9500/ws/agent", true},
		{"ws://relay.example.com:9500/ws/agent", false},
		{"not-a-url", false},
	}
	for _, tc := range cases {
		if got := IsLoopbackURL(tc.raw); got != tc.want {
			t.Fatalf("IsLoopbackURL(%q) = %v, want %v", tc.raw, got, tc.want)
		}
	}
}

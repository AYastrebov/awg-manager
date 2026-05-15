package server

import "testing"

func TestSkipSlowRequestLog(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"/debug/pprof/", true},
		{"/debug/pprof/heap", true},
		{"/api/events", true},
		{"/api/singbox/clash/proxies", true},
		{"/api/singbox/clash/logs", true},
		{"/api/health", false},
		{"/api/tunnels/all", false},
	}
	for _, tc := range cases {
		if got := skipSlowRequestLog(tc.path); got != tc.want {
			t.Fatalf("skipSlowRequestLog(%q): got %v want %v", tc.path, got, tc.want)
		}
	}
}

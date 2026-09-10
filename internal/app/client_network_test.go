package app

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// 覆盖网段边界、双栈地址以及恶意转发头，确保实际连接来源才是授权依据。
func TestRestrictClientNetworks(t *testing.T) {
	prefixes, err := parseAllowedClientCIDRs([]string{"192.168.10.0/24", "2001:db8:1::/48", "::ffff:10.0.0.0/104"})
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{allowedClientPrefixes: prefixes}
	handler := server.restrictClientNetworks(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	for _, tc := range []struct {
		name, remote string
		status       int
	}{
		{"IPv4 allowed", "192.168.10.25:1234", 204},
		{"IPv4 lower boundary", "192.168.10.0:1234", 204},
		{"IPv4 upper boundary", "192.168.10.255:1234", 204},
		{"IPv4 outside", "192.168.11.0:1234", 403},
		{"IPv6 allowed", "[2001:db8:1::1]:1234", 204},
		{"IPv6 outside", "[2001:db8:2::1]:1234", 403},
		{"mapped IPv4 address", "[::ffff:192.168.10.25]:1234", 204},
		{"mapped IPv4 network", "10.1.2.3:1234", 204},
		{"loopback not implicitly allowed", "127.0.0.1:1234", 403},
		{"malformed address", "invalid", 403},
		{"malformed host", "invalid:1234", 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tc.remote
			// 不论真实来源是否允许，这些由调用者控制的请求头都不能改变授权结果。
			req.Header.Set("X-Forwarded-For", "192.168.10.1")
			req.Header.Set("X-Real-IP", "192.168.10.1")
			req.Header.Set("Forwarded", "for=192.168.10.1")
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)
			if recorder.Code != tc.status {
				t.Fatalf("status=%d, want %d", recorder.Code, tc.status)
			}
		})
	}
}

func TestClientNetworkRestrictionCoversAllRoutes(t *testing.T) {
	server := newWebhookTestServer(t)
	server.allowedClientPrefixes, _ = parseAllowedClientCIDRs([]string{"10.0.0.0/8"})
	for _, path := range []string{"/", "/api/health", "/api/repos", "/api/webhooks/github/secret", "/api/webhooks/github/12"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.RemoteAddr = "192.0.2.1:1234"
		recorder := httptest.NewRecorder()
		server.Routes().ServeHTTP(recorder, req)
		if recorder.Code != http.StatusForbidden {
			t.Fatalf("%s status=%d", path, recorder.Code)
		}
	}
	// 默认未配置时维持兼容，任意地址均可访问健康检查。
	server.allowedClientPrefixes = nil
	recorder := httptest.NewRecorder()
	server.Routes().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("unrestricted health status=%d", recorder.Code)
	}
}

func TestAllowedClientCIDRsConfig(t *testing.T) {
	for _, raw := range []string{"", "10.0.0.0/8, ::1/128", "bad", "10.0.0.0/33", ",", "10.0.0.0/8,", "::ffff:10.0.0.0/80"} {
		t.Run(raw, func(t *testing.T) {
			t.Setenv("ALLOWED_CLIENT_CIDRS", raw)
			cfg := LoadConfig()
			_, err := parseAllowedClientCIDRs(cfg.AllowedClientCIDRs)
			valid := raw == "" || raw == "10.0.0.0/8, ::1/128"
			if (err == nil) != valid {
				t.Fatalf("validation error=%v, valid=%v", err, valid)
			}
			if !valid {
				// 必须在打开数据库等启动副作用之前返回配置错误。
				if _, err := NewServer(Config{AllowedClientCIDRs: cfg.AllowedClientCIDRs}); err == nil {
					t.Fatal("invalid network configuration did not prevent startup")
				}
			}
		})
	}
}

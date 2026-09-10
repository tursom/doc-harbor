package app

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// parseAllowedClientCIDRs 在启动时严格校验所有条目，避免跳过错误网段后意外放开访问。
// 支持 IPv4/IPv6 CIDR；单个主机请明确填写 /32 或 /128。
func parseAllowedClientCIDRs(values []string) ([]netip.Prefix, error) {
	prefixes := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(value))
		if err != nil {
			return nil, fmt.Errorf("invalid ALLOWED_CLIENT_CIDRS entry %q: %w", value, err)
		}
		// 将映射为 IPv6 的 IPv4 网段统一为 IPv4，与连接地址的 Unmap 保持一致。
		if prefix.Addr().Is4In6() {
			if prefix.Bits() < 96 {
				return nil, fmt.Errorf("invalid ALLOWED_CLIENT_CIDRS entry %q: mapped IPv4 prefix must be at least /96", value)
			}
			prefix = netip.PrefixFrom(prefix.Addr().Unmap(), prefix.Bits()-96)
		}
		prefixes = append(prefixes, prefix.Masked())
	}
	return prefixes, nil
}

// restrictClientNetworks 对所有路径统一执行来源限制，包括静态页面、健康检查和 Webhook。
// 只使用 HTTP 服务提供的连接对端地址，不读取 X-Forwarded-For、X-Real-IP 或 Forwarded，
// 防止客户端自行设置请求头绕过白名单。反向代理后的原始访客限制应由代理层执行。
func (s *Server) restrictClientNetworks(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(s.allowedClientPrefixes) == 0 {
			next.ServeHTTP(w, r)
			return
		}
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err == nil {
			addr, parseErr := netip.ParseAddr(host)
			if parseErr == nil {
				// 去掉 IPv6 接口区域标识，再将 IPv4 映射地址规范化后匹配网段。
				addr = addr.WithZone("").Unmap()
				for _, prefix := range s.allowedClientPrefixes {
					if prefix.Contains(addr) {
						next.ServeHTTP(w, r)
						return
					}
				}
			}
		}
		// 无法解析的连接来源也拒绝，且禁止代理缓存拒绝响应后影响其他客户端。
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "client address is not allowed"})
	})
}

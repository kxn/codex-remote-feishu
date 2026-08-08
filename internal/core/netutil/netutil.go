// Package netutil 提供网络判定原语（loopback 识别）的单一实现。
// 所有包（adminauth / externalaccess / runtime / daemon startup）的
// loopback 判定必须委托本包，禁止复制 localhost / IP.IsLoopback 判定序列。
package netutil

import (
	"net"
	"net/http"
	"net/url"
	"strings"
)

// IsLoopbackHost 报告 host（已剥离端口）是否为 loopback 地址：
// localhost（大小写不敏感）或 IPv4/IPv6 loopback。
func IsLoopbackHost(host string) bool {
	host = strings.TrimSpace(host)
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// IsLoopbackAddress 从 addr（可能为 host:port 或纯 host）提取 host 后判定。
func IsLoopbackAddress(addr string) bool {
	host := strings.TrimSpace(addr)
	if parsedHost, _, err := net.SplitHostPort(host); err == nil {
		host = parsedHost
	}
	return IsLoopbackHost(host)
}

// IsLoopbackRequest 报告 HTTP 请求来源地址是否为 loopback。
func IsLoopbackRequest(r *http.Request) bool {
	return r != nil && IsLoopbackAddress(r.RemoteAddr)
}

// IsLoopbackURL 报告 URL 的主机是否为 loopback。
func IsLoopbackURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return IsLoopbackHost(parsed.Hostname())
}

// IsLANHost reports whether host is a private LAN address suitable for a
// user-selected local-network listener.
func IsLANHost(host string) bool {
	host = strings.Trim(strings.TrimSpace(host), "[]")
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsPrivate() &&
		!ip.IsLoopback() &&
		!ip.IsLinkLocalUnicast() &&
		!ip.IsUnspecified() &&
		!ip.IsMulticast()
}

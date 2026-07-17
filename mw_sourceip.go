package wshell

import (
	"net"
	"net/netip"
	"strings"

	"github.com/labstack/echo/v4"
)

func SourceIP() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Try Cloudflare header first
			ip := strings.TrimSpace(c.Request().Header.Get("CF-Connecting-IP"))
			if ip != "" {
				ip = canonicalIP(ip)
			} else {
				// Fallback to X-Forwarded-For
				forwardedFor := c.Request().Header.Get("X-Forwarded-For")
				if idx := strings.LastIndex(forwardedFor, ","); idx != -1 {
					forwardedFor = forwardedFor[idx+1:]
				}
				forwardedFor = strings.TrimSpace(forwardedFor)

				if forwardedFor != "" {
					ip = canonicalIP(forwardedFor)
				} else {
					// Final fallback to RemoteAddr
					ip = canonicalRemoteIP(c.Request().RemoteAddr)
				}
			}

			c.Set(ContextKey_SourceIP, ip)
			return next(c)
		}
	}
}

func canonicalRemoteIP(remoteAddr string) string {
	remoteAddr = strings.TrimSpace(remoteAddr)
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
		remoteAddr = host
	}

	return canonicalIP(remoteAddr)
}

func canonicalIP(value string) string {
	addr, err := netip.ParseAddr(strings.TrimSpace(value))
	if err != nil {
		return ""
	}

	return addr.Unmap().String()
}

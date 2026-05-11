package wshell

import (
	"strings"

	"github.com/labstack/echo/v4"
)

func SourceIP() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Try Cloudflare header first
			ip := c.Request().Header.Get("CF-Connecting-IP")
			if ip == "" {
				// Fallback to X-Forwarded-For
				ip = c.Request().Header.Get("X-Forwarded-For")
				if ip != "" {
					if idx := strings.LastIndex(ip, ","); idx != -1 {
						ip = ip[idx+1:]
					}
					ip = strings.TrimSpace(ip)
				}

				if ip == "" {
					// Final fallback to RemoteAddr
					ip = c.Request().RemoteAddr
					if idx := strings.LastIndex(ip, ":"); idx != -1 {
						ip = ip[:idx] + "/32"
					} else {
						ip = ip + "/32"
					}
				}
			}

			c.Set(ContextKey_SourceIP, ip)
			return next(c)
		}
	}
}

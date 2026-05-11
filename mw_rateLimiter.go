package wshell

import (
	"sync"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/redis/go-redis/v9"
)

type inMemoryBucket struct {
	count   int64
	resetAt time.Time
}

type inMemoryLimiter struct {
	mu      sync.Mutex
	buckets map[string]*inMemoryBucket
}

func newInMemoryLimiter() *inMemoryLimiter {
	return &inMemoryLimiter{
		buckets: make(map[string]*inMemoryBucket),
	}
}

func (l *inMemoryLimiter) allow(key string, maxRequests int64, window time.Duration) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	bucket, ok := l.buckets[key]
	if !ok || now.After(bucket.resetAt) {
		l.buckets[key] = &inMemoryBucket{
			count:   1,
			resetAt: now.Add(window),
		}
		return true
	}

	if bucket.count >= maxRequests {
		return false
	}

	bucket.count++
	return true
}

/*
Request limiter will limit the number of requests from a single IP address

	As single instance redis is not required as the stats are in-memory
	On multiple instances with a loadbalancer, a distributed cache like redis is required
*/
func RateLimiter(rdb *redis.Client, maxRequests int, duration time.Duration) echo.MiddlewareFunc {
	localLimiter := newInMemoryLimiter()
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			sourceIP := c.Get(ContextKey_SourceIP).(string)
			if sourceIP == "" {
				sourceIP = c.Request().RemoteAddr
			}

			// If redis is available prefer redis
			if rdb != nil {
				ctx := c.Request().Context()
				key := "rate_limit:" + sourceIP

				count64, err := rdb.Incr(ctx, key).Result()
				count := int(count64)
				if err == nil && count == 1 {
					if expireErr := rdb.Expire(ctx, key, duration).Err(); expireErr != nil {
						_, _ = rdb.Del(ctx, key).Result()
						err = expireErr
					}
				}

				if err == nil {
					if count > maxRequests {
						return echo.NewHTTPError(429, "Rate limit exceeded")
					}
					return next(c)
				}
			}

			// Fallback to in-memory rate limiting
			// ! This will not work as expected  on multiple server instances. This pod wont be aware of the requests made to other pods.
			if !localLimiter.allow(sourceIP, int64(maxRequests), duration) {
				return echo.NewHTTPError(429, "Rate limit exceeded")
			}

			return next(c)
		}
	}
}

package app

import (
	"math"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
)

const (
	loginLimitWindow      = 5 * time.Minute
	loginLimitAttempts    = 10
	loginLimitBucketLimit = 4096
)

type loginLimitBucket struct {
	startedAt time.Time
	attempts  int
}

type loginLimiter struct {
	mu      sync.Mutex
	buckets map[string]loginLimitBucket
}

func (limiter *loginLimiter) allow(remoteAddr string, now time.Time) time.Duration {
	ip := normalizedRemoteIP(remoteAddr)

	limiter.mu.Lock()
	defer limiter.mu.Unlock()

	if limiter.buckets == nil {
		limiter.buckets = make(map[string]loginLimitBucket)
	}
	if bucket, ok := limiter.buckets[ip]; ok {
		if now.Before(bucket.startedAt) {
			return bucket.startedAt.Add(loginLimitWindow).Sub(now)
		}
		remaining := bucket.startedAt.Add(loginLimitWindow).Sub(now)
		if remaining > 0 {
			if bucket.attempts >= loginLimitAttempts {
				return remaining
			}
			bucket.attempts++
			limiter.buckets[ip] = bucket
			return 0
		}
		delete(limiter.buckets, ip)
	}

	for key, bucket := range limiter.buckets {
		if !now.Before(bucket.startedAt) && now.Sub(bucket.startedAt) >= loginLimitWindow {
			delete(limiter.buckets, key)
		}
	}
	if len(limiter.buckets) >= loginLimitBucketLimit {
		return loginLimitWindow
	}
	limiter.buckets[ip] = loginLimitBucket{startedAt: now, attempts: 1}
	return 0
}

func normalizedRemoteIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return "unknown"
	}
	if ipv4 := ip.To4(); ipv4 != nil {
		return ipv4.String()
	}
	return ip.String()
}

func (s *Server) allowLoginAttempt(w http.ResponseWriter, r *http.Request) bool {
	// The disposable E2E fixture performs many logins from one loopback
	// address within seconds; production behavior is unchanged.
	if os.Getenv("VIDEO_SHARE_E2E") == "1" {
		return true
	}
	retryAfter := s.loginLimiter.allow(r.RemoteAddr, time.Now().UTC())
	if retryAfter <= 0 {
		return true
	}
	seconds := int(math.Ceil(retryAfter.Seconds()))
	if seconds < 1 {
		seconds = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	http.Error(w, "Too many authentication attempts", http.StatusTooManyRequests)
	return false
}

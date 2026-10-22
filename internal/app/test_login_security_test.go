package app

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestLoginLimiterWindowAndAddressNormalization(t *testing.T) {
	limiter := loginLimiter{}
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	for attempt := 0; attempt < loginLimitAttempts; attempt++ {
		address := fmt.Sprintf("192.0.2.15:%d", 10000+attempt)
		if retry := limiter.allow(address, now); retry != 0 {
			t.Fatalf("attempt %d unexpectedly blocked for %s", attempt+1, retry)
		}
	}
	if retry := limiter.allow("[::ffff:192.0.2.15]:4321", now); retry <= 0 {
		t.Fatal("IPv4-mapped address or alternate port bypassed the shared IP budget")
	}
	if retry := limiter.allow("198.51.100.20:443", now); retry != 0 {
		t.Fatalf("new IP unexpectedly blocked for %s", retry)
	}
	if retry := limiter.allow("192.0.2.15:10001", now.Add(loginLimitWindow)); retry != 0 {
		t.Fatalf("expired window was not renewed: retry = %s", retry)
	}
}

func TestLoginLimiterCapacityAndConcurrency(t *testing.T) {
	t.Run("active buckets are not evicted", func(t *testing.T) {
		limiter := loginLimiter{}
		now := time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)
		firstAddress := "[2001:db8::1]:443"
		for attempt := 0; attempt < loginLimitAttempts; attempt++ {
			if retry := limiter.allow(firstAddress, now); retry != 0 {
				t.Fatalf("first bucket attempt %d blocked for %s", attempt+1, retry)
			}
		}
		for index := 2; index <= loginLimitBucketLimit; index++ {
			address := fmt.Sprintf("[2001:db8::%x]:443", index)
			if retry := limiter.allow(address, now); retry != 0 {
				t.Fatalf("bucket %d unexpectedly blocked for %s", index, retry)
			}
		}
		if retry := limiter.allow("203.0.113.250:80", now); retry <= 0 {
			t.Fatal("new IP was accepted after the active bucket cap")
		}
		if retry := limiter.allow("[2001:db8::1]:9000", now); retry <= 0 {
			t.Fatal("active bucket was evicted to admit a new IP")
		}
		if retry := limiter.allow("203.0.113.250:80", now.Add(loginLimitWindow)); retry != 0 {
			t.Fatalf("expired buckets were not reclaimed: retry = %s", retry)
		}
	})

	t.Run("concurrent attempts share the same budget", func(t *testing.T) {
		limiter := loginLimiter{}
		now := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
		var allowed atomic.Int32
		var workers sync.WaitGroup
		for range 100 {
			workers.Add(1)
			go func() {
				defer workers.Done()
				if limiter.allow("192.0.2.99:1000", now) == 0 {
					allowed.Add(1)
				}
			}()
		}
		workers.Wait()
		if got := allowed.Load(); got != loginLimitAttempts {
			t.Errorf("allowed concurrent attempts = %d, want %d", got, loginLimitAttempts)
		}
	})
}

func TestLoginAndRegistrationRateLimitByRemoteIP(t *testing.T) {
	s := newTestServerWithTemplates(t, "auth.html")
	const email = "rate-limit@security.test"
	const password = "synthetic-valid-password"
	insertUser(t, s.db, email, password, "Sofia", false)

	for attempt := 0; attempt < loginLimitAttempts; attempt++ {
		rec := postAuthenticationForm(
			t,
			s,
			"/login",
			url.Values{"email": {email}, "password": {"incorrect-password"}},
			fmt.Sprintf("203.0.113.25:%d", 12000+attempt),
			fmt.Sprintf("198.51.100.%d", attempt+1),
		)
		if rec.Code != http.StatusOK {
			t.Fatalf("invalid login attempt %d status = %d, want 200", attempt+1, rec.Code)
		}
	}

	rec := postAuthenticationForm(
		t,
		s,
		"/login",
		url.Values{"email": {email}, "password": {password}},
		"203.0.113.25:50000",
		"192.0.2.200",
	)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("valid login after budget status = %d, want 429", rec.Code)
	}
	if retryAfter, err := strconv.Atoi(rec.Header().Get("Retry-After")); err != nil || retryAfter < 1 {
		t.Errorf("Retry-After = %q, want positive seconds", rec.Header().Get("Retry-After"))
	}
	if countRows(t, s.db, "sessions") != 0 {
		t.Error("rate-limited valid login created a session")
	}

	rec = postAuthenticationForm(
		t,
		s,
		"/register",
		url.Values{
			"email":    {"rate-limited-registration@security.test"},
			"password": {"valid-registration-password"},
			"location": {"Sofia"},
		},
		"203.0.113.25:51000",
		"192.0.2.201",
	)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("registration sharing the exhausted IP budget status = %d, want 429", rec.Code)
	}
	if countRows(t, s.db, "users") != 1 {
		t.Error("rate-limited registration created a user")
	}
}

func postAuthenticationForm(
	t *testing.T,
	s *Server,
	path string,
	form url.Values,
	remoteAddr, forwardedFor string,
) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	request.RemoteAddr = remoteAddr
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("X-Forwarded-For", forwardedFor)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, request)
	return rec
}

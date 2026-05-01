package middleware

import (
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/httpresponse"
	"github.com/gin-gonic/gin"
)

type BodyLimitConfig struct {
	MaxJSONBodyBytes int64
}

func BodySizeLimit(cfg BodyLimitConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		if cfg.MaxJSONBodyBytes > 0 && requestHasJSONBody(c.Request) {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, cfg.MaxJSONBodyBytes)
		}
		c.Next()
	}
}

func requestHasJSONBody(req *http.Request) bool {
	if req.Body == nil || req.Body == http.NoBody {
		return false
	}
	if req.Method == http.MethodGet || req.Method == http.MethodHead || req.Method == http.MethodDelete {
		return false
	}
	contentType := req.Header.Get("Content-Type")
	return strings.HasPrefix(strings.ToLower(contentType), "application/json")
}

type RateLimitConfig struct {
	Requests int
	Window   time.Duration
}

type RateLimiter struct {
	mu      sync.Mutex
	buckets map[string]rateBucket
}

type rateBucket struct {
	count     int
	resetAt   time.Time
	lastSeen  time.Time
	overLimit bool
}

func NewRateLimiter() *RateLimiter {
	return &RateLimiter{buckets: make(map[string]rateBucket)}
}

func RateLimit(limiter *RateLimiter, cfg RateLimitConfig, bucketName string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if limiter == nil || cfg.Requests <= 0 || cfg.Window <= 0 {
			c.Next()
			return
		}

		key := bucketName + ":" + c.ClientIP()
		if !limiter.Allow(key, cfg.Requests, cfg.Window) {
			httpresponse.Respond(c, http.StatusTooManyRequests, apperrors.New(apperrors.ErrResourceExhausted, "rate limit exceeded"))
			return
		}
		c.Next()
	}
}

func (l *RateLimiter) Allow(key string, limit int, window time.Duration) bool {
	now := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()

	l.cleanupLocked(now, window)

	bucket := l.buckets[key]
	if bucket.resetAt.IsZero() || now.After(bucket.resetAt) {
		bucket = rateBucket{resetAt: now.Add(window)}
	}
	bucket.count++
	bucket.lastSeen = now
	bucket.overLimit = bucket.count > limit
	l.buckets[key] = bucket

	return !bucket.overLimit
}

func (l *RateLimiter) cleanupLocked(now time.Time, window time.Duration) {
	for key, bucket := range l.buckets {
		if bucket.lastSeen.IsZero() || now.Sub(bucket.lastSeen) > 2*window {
			delete(l.buckets, key)
		}
	}
}

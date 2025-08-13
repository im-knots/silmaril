package torrent

import (
	"golang.org/x/time/rate"
)

// NewRateLimiter creates a new rate limiter with the specified bytes per second limit
func NewRateLimiter(bytesPerSecond int64) *rate.Limiter {
	// Convert bytes per second to tokens per second
	// Each token represents one byte
	return rate.NewLimiter(rate.Limit(bytesPerSecond), int(bytesPerSecond))
}
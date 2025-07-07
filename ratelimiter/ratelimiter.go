package ratelimiter

import (
	"net"
	"net/http"
	"sync"

	"golang.org/x/time/rate"
)

type IPRateLimiter struct {
	ips   map[string]*rate.Limiter
	mu    *sync.RWMutex
	limit rate.Limit
	burst int
}

func NewIPRateLimiter(limit rate.Limit, burst int) *IPRateLimiter {
	return &IPRateLimiter{
		ips:   make(map[string]*rate.Limiter),
		mu:    &sync.RWMutex{},
		limit: limit,
		burst: burst,
	}
}

func (i *IPRateLimiter) GetLimiter(ip string) *rate.Limiter {
	i.mu.Lock()
	defer i.mu.Unlock()

	limiter, exists := i.ips[ip]
	if !exists {
		limiter = rate.NewLimiter(i.limit, i.burst)
		i.ips[ip] = limiter
	}
	return limiter
}

func GetClientIP(r *http.Request) string {
	ip := r.RemoteAddr
	if host, _, err := net.SplitHostPort(ip); err == nil {
		ip = host
	}
	return ip
}

// Less of a rate limiter and more of a total connection per IP tracker
type ConnectionLimiter struct {
	ips   map[string]int
	mu    *sync.RWMutex
	limit int
}

func NewConnectionLimiter(limit int) *ConnectionLimiter {
	return &ConnectionLimiter{
		ips:   make(map[string]int),
		mu:    &sync.RWMutex{},
		limit: limit,
	}
}

func (c *ConnectionLimiter) AddConnection(ip string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	count, _ := c.ips[ip]
	if count >= c.limit {
		return false
	}
	c.ips[ip] = count + 1
	return true
}

func (c *ConnectionLimiter) RemoveConnection(ip string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if count, exists := c.ips[ip]; exists {
		if count > 1 {
			c.ips[ip] = count - 1
		} else {
			delete(c.ips, ip)
		}
	}
}

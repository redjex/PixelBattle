package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

var operationIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

type telegramUserKey struct{}
type trustedProxiesKey struct{}

type appAccessPolicy interface {
	IsAdmin(int64) bool
	IsTestMode() bool
}

func parseTrustedProxies(raw string) ([]netip.Prefix, error) {
	var prefixes []netip.Prefix
	if strings.TrimSpace(raw) == "" {
		return prefixes, nil
	}
	for _, value := range strings.Split(raw, ",") {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(value))
		if err != nil || prefix.Addr().Is4In6() {
			return nil, fmt.Errorf("TRUSTED_PROXY_CIDRS must contain comma-separated IPv4 or IPv6 CIDRs")
		}
		prefixes = append(prefixes, prefix.Masked())
	}
	return prefixes, nil
}

// Trust only explicitly configured immediate peers. Ingress must overwrite
// X-Real-IP (nginx: proxy_set_header X-Real-IP $remote_addr), not forward a
// client-supplied value. Any outer proxy must sanitize its own trust boundary.
func withTrustedProxies(next http.Handler, prefixes []netip.Prefix) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), trustedProxiesKey{}, prefixes)))
	})
}

type connectionLimits struct {
	mu    sync.Mutex
	total int
	byIP  map[string]int
}

func (l *connectionLimits) acquire(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.total >= 2000 || l.byIP[ip] >= 50 {
		return false
	}
	if l.byIP == nil {
		l.byIP = make(map[string]int)
	}
	l.total++
	l.byIP[ip]++
	return true
}

func (l *connectionLimits) release(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.byIP[ip] == 0 {
		return
	}
	l.total--
	l.byIP[ip]--
	if l.byIP[ip] == 0 {
		delete(l.byIP, ip)
	}
}

func adminAuthorized(r *http.Request, token string) bool {
	values := r.Header.Values("Authorization")
	if token == "" || strings.TrimSpace(token) != token || len(values) != 1 {
		return false
	}
	scheme, supplied, ok := strings.Cut(values[0], " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || supplied == "" || strings.ContainsAny(supplied, " \t\r\n") {
		return false
	}
	expectedHash, suppliedHash := sha256.Sum256([]byte(token)), sha256.Sum256([]byte(supplied))
	return subtle.ConstantTimeCompare(expectedHash[:], suppliedHash[:]) == 1
}

// Fixed windows expire lazily; a full table rejects new identities rather than
// evicting active limits. Arbitrary paths are never keys; proxy IPs are validated.
type rateEntry struct {
	until time.Time
	count int
}
type rateLimiter struct {
	mu        sync.Mutex
	entries   map[string]rateEntry
	nextSweep time.Time
}

func (l *rateLimiter) allow(key string, limit int, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.entries == nil {
		l.entries = make(map[string]rateEntry)
	}
	if !now.Before(l.nextSweep) {
		for key, entry := range l.entries {
			if !now.Before(entry.until) {
				delete(l.entries, key)
			}
		}
		l.nextSweep = now.Add(time.Minute)
	}
	entry, exists := l.entries[key]
	if !exists || !now.Before(entry.until) {
		if !exists && len(l.entries) >= 10000 {
			return false
		}
		entry = rateEntry{until: now.Add(time.Minute)}
	}
	if entry.count >= limit {
		return false
	}
	entry.count++
	l.entries[key] = entry
	return true
}

func peerIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil || peer.Zone() != "" {
		return "invalid-peer"
	}
	peer = peer.Unmap()
	prefixes, _ := r.Context().Value(trustedProxiesKey{}).([]netip.Prefix)
	for _, prefix := range prefixes {
		if !prefix.Contains(peer) {
			continue
		}
		values := r.Header.Values("X-Real-IP")
		if len(values) != 1 {
			break
		}
		ip, err := netip.ParseAddr(values[0])
		if err != nil || ip.Zone() != "" || ip.IsUnspecified() || ip.IsMulticast() {
			break
		}
		return ip.Unmap().String()
	}
	return peer.String()
}

func allowedMethods(path string) string {
	switch path {
	case "/health", "/inline-map.jpg", "/ws", "/api/boards/main", "/api/boards/session", "/api/boards/main/stats", "/api/profiles/me", "/api/boards/main/image", "/api/admin/stats", "/api/admin/recording", "/api/admin/captcha/statuses", "/api/admin/trophy-reward-requests", "/api/admin/trophy-chat-notifications":
		return "GET"
	case "/api/boards/main/rewards", "/api/boards/captcha", "/api/admin/trophies/drop":
		return "GET, POST"
	case "/api/admin/boards/main/size", "/api/admin/game/pause", "/api/admin/game/test-mode", "/api/profiles/me/privacy", "/api/boards/profiles/me/privacy":
		return "GET, PUT"
	case "/api/boards/main/pixels", "/api/boards/items/ice/activate", "/api/boards/items/bomb/use", "/api/admin/boards/main/fill", "/api/admin/boards/main/image", "/api/admin/boards/main/clear", "/api/admin/boards/main/restore", "/api/admin/quests/reset", "/api/admin/items/grant", "/api/admin/trophies/reset", "/api/admin/captcha/require", "/api/admin/trophy-reward-requests/ack", "/api/admin/trophy-chat-notifications/ack", "/api/admin/recording/start", "/api/admin/recording/stop", "/api/admin/reset-all":
		return "POST"
	}
	if strings.HasPrefix(path, "/api/profiles/") || strings.HasPrefix(path, "/api/boards/profiles/") {
		return "GET"
	}
	if strings.HasPrefix(path, "/api/boards/trophy-items/") && strings.HasSuffix(path, "/claim") {
		return "POST"
	}
	if strings.HasPrefix(path, "/api/boards/trophies/") && strings.HasSuffix(path, "/reward") {
		return "GET"
	}
	if strings.HasPrefix(path, "/api/boards/trophies/") && strings.HasSuffix(path, "/request") {
		return "POST"
	}
	return ""
}

func maintenanceAllows(path string, userID int64, policy appAccessPolicy) bool {
	return policy == nil || !policy.IsTestMode() || policy.IsAdmin(userID) || path == "/api/boards/session"
}

func secureHandler(next http.Handler, adminToken string, limits *rateLimiter, policy appAccessPolicy) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if !limits.allow("ip:"+peerIP(r), 600, time.Now()) {
			w.Header().Set("Retry-After", "60")
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		methods := allowedMethods(r.URL.Path)
		if methods == "" {
			http.NotFound(w, r)
			return
		}
		if !strings.Contains(", "+methods+", ", ", "+r.Method+", ") {
			w.Header().Set("Allow", methods)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/admin/") || r.URL.Path == "/api/boards/main/image" {
			if !adminAuthorized(r, adminToken) {
				w.Header().Set("WWW-Authenticate", "Bearer")
				http.Error(w, "admin access required", http.StatusUnauthorized)
				return
			}
		} else if strings.HasPrefix(r.URL.Path, "/api/") {
			user, err := telegramUserFromRequest(r)
			if err != nil {
				http.Error(w, "Telegram Mini App authentication required", http.StatusUnauthorized)
				return
			}
			if !limits.allow("user:"+strconv.FormatInt(user.ID, 10), 240, time.Now()) {
				w.Header().Set("Retry-After", "60")
				http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
				return
			}
			if !maintenanceAllows(r.URL.Path, user.ID, policy) {
				http.Error(w, "test mode enabled", http.StatusLocked)
				return
			}
			r = r.WithContext(context.WithValue(r.Context(), telegramUserKey{}, user))
		}
		limit := int64(16 << 10)
		if r.URL.Path == "/api/admin/boards/main/image" {
			limit = 32 << 20
		}
		if r.ContentLength > limit {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, limit)
			body, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, "request body too large or unreadable", http.StatusRequestEntityTooLarge)
				return
			}
			if len(body) > 0 && (r.Method == http.MethodGet || !json.Valid(body)) {
				http.Error(w, "invalid request body", http.StatusBadRequest)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
		}
		next.ServeHTTP(w, r)
	})
}

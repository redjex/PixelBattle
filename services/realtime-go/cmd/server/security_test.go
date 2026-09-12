package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"pixelbattle/realtime/internal/domain"
	"pixelbattle/realtime/internal/state"
)

func TestTrustedProxyIP(t *testing.T) {
	trusted, err := parseTrustedProxies("192.0.2.0/24, 2001:db8::/32")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, peer string
		headers    []string
		trust      bool
		want       string
	}{
		{"default none", "192.0.2.1:123", []string{"198.51.100.1"}, false, "192.0.2.1"},
		{"trusted", "192.0.2.1:123", []string{"198.51.100.1"}, true, "198.51.100.1"},
		{"untrusted", "203.0.113.1:123", []string{"198.51.100.1"}, true, "203.0.113.1"},
		{"ipv6 canonical", "[2001:db8::1]:123", []string{"2001:0db8:0001::2"}, true, "2001:db8:1::2"},
		{"mapped peer", "[::ffff:192.0.2.1]:123", []string{"::ffff:198.51.100.1"}, true, "198.51.100.1"},
		{"duplicate", "192.0.2.1:123", []string{"198.51.100.1", "198.51.100.1"}, true, "192.0.2.1"},
		{"list", "192.0.2.1:123", []string{"198.51.100.1, 203.0.113.1"}, true, "192.0.2.1"},
		{"port", "192.0.2.1:123", []string{"198.51.100.1:80"}, true, "192.0.2.1"},
		{"zone", "192.0.2.1:123", []string{"fe80::1%eth0"}, true, "192.0.2.1"},
		{"whitespace", "192.0.2.1:123", []string{" 198.51.100.1"}, true, "192.0.2.1"},
		{"invalid", "192.0.2.1:123", []string{"not-an-ip"}, true, "192.0.2.1"},
		{"missing", "192.0.2.1:123", nil, true, "192.0.2.1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/ws", nil)
			r.RemoteAddr = test.peer
			for _, value := range test.headers {
				r.Header.Add("X-Real-IP", value)
			}
			r.Header.Set("X-Forwarded-For", "203.0.113.99")
			if test.trust {
				r = r.WithContext(context.WithValue(r.Context(), trustedProxiesKey{}, trusted))
			}
			if got := peerIP(r); got != test.want {
				t.Fatalf("got %s want %s", got, test.want)
			}
		})
	}
	for _, raw := range []string{"not-cidr", "192.0.2.1", "192.0.2.0/24,", "::ffff:192.0.2.0/120"} {
		if _, err := parseTrustedProxies(raw); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
	if prefixes, err := parseTrustedProxies(""); err != nil || len(prefixes) != 0 {
		t.Fatal("default trust not empty")
	}
}

func TestProxyResolutionSharedByMiddlewareAndHandler(t *testing.T) {
	trusted, _ := parseTrustedProxies("192.0.2.0/24")
	limits := &rateLimiter{}
	h := withTrustedProxies(secureHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if peerIP(r) != "198.51.100.1" {
			t.Fatal("handler lost proxy configuration")
		}
		w.WriteHeader(204)
	}), "", limits, nil), trusted)
	r := httptest.NewRequest("GET", "/ws", nil)
	r.RemoteAddr = "192.0.2.1:123"
	r.Header.Set("X-Real-IP", "198.51.100.1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 204 || limits.entries["ip:198.51.100.1"].count != 1 {
		t.Fatal("middleware used wrong IP")
	}
}

func TestAdminAuthorization(t *testing.T) {
	for _, test := range []struct {
		name, token, header, query string
		want                       bool
	}{
		{"valid", "secret", "Bearer secret", "", true},
		{"case insensitive scheme", "secret", "bearer secret", "", true},
		{"query only", "secret", "", "?adminToken=secret", false},
		{"wrong header with valid query", "secret", "Bearer wrong", "?adminToken=secret", false},
		{"empty configuration", "", "Bearer ", "", false},
		{"blank configuration", " ", "Bearer  ", "", false},
		{"basic", "secret", "Basic secret", "", false},
		{"extra space", "secret", "Bearer  secret", "", false},
		{"trailing space", "secret", "Bearer secret ", "", false},
		{"combined headers", "secret", "Bearer secret, Bearer secret", "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/api/admin/stats"+test.query, nil)
			if test.header != "" {
				r.Header.Set("Authorization", test.header)
			}
			if got := adminAuthorized(r, test.token); got != test.want {
				t.Fatalf("authorized=%v", got)
			}
		})
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Add("Authorization", "Bearer secret")
	r.Header.Add("Authorization", "Bearer secret")
	if adminAuthorized(r, "secret") {
		t.Fatal("duplicate headers accepted")
	}
}

func TestSecurityMiddleware(t *testing.T) {
	for _, test := range []struct {
		name, method, path, body, header string
		chunked                          bool
		status                           int
	}{
		{"query token rejected", "GET", "/api/admin/stats?adminToken=secret", "", "", false, 401},
		{"bearer works", "GET", "/api/admin/stats", "", "Bearer secret", false, 204},
		{"trophy status", "GET", "/api/admin/trophies/drop", "", "Bearer secret", false, 204},
		{"trophy force ready", "POST", "/api/admin/trophies/drop", `{}`, "Bearer secret", false, 204},
		{"trophy method rejected", "PUT", "/api/admin/trophies/drop", `{}`, "Bearer secret", false, 405},
		{"trophy reset", "POST", "/api/admin/trophies/reset", `{"userId":"123"}`, "Bearer secret", false, 204},
		{"force captcha", "POST", "/api/admin/captcha/require", `{"userId":"123"}`, "Bearer secret", false, 204},
		{"captcha statuses", "GET", "/api/admin/captcha/statuses", "", "Bearer secret", false, 204},
		{"reward requests", "GET", "/api/admin/trophy-reward-requests", "", "Bearer secret", false, 204},
		{"reward request ack", "POST", "/api/admin/trophy-reward-requests/ack", `{"requestId":1}`, "Bearer secret", false, 204},
		{"recording status", "GET", "/api/admin/recording", "", "Bearer secret", false, 204},
		{"recording start", "POST", "/api/admin/recording/start", `{}`, "Bearer secret", false, 204},
		{"recording stop", "POST", "/api/admin/recording/stop", `{}`, "Bearer secret", false, 204},
		{"reset all", "POST", "/api/admin/reset-all", `{}`, "Bearer secret", false, 204},
		{"public inline map", "GET", "/inline-map.jpg", "", "", false, 204},
		{"resize requires put", "POST", "/api/admin/boards/main/size", `{}`, "Bearer secret", false, 405},
		{"resize put", "PUT", "/api/admin/boards/main/size", `{}`, "Bearer secret", false, 204},
		{"get only", "POST", "/api/profiles/123", "", "", false, 405},
		{"trophy reward requires get", "POST", "/api/boards/trophies/yng-explrz/reward", `{}`, "", false, 405},
		{"trophy reward requires auth", "GET", "/api/boards/trophies/yng-explrz/reward", "", "", false, 401},
		{"no implicit head", "HEAD", "/health", "", "", false, 405},
		{"unknown route", "POST", "/unknown", "", "", false, 404},
		{"auth before body", "POST", "/api/boards/main/pixels", `{}`, "", false, 401},
		{"oversized", "PUT", "/api/admin/boards/main/size", strings.Repeat(" ", 16385), "Bearer secret", false, 413},
		{"oversized chunked", "PUT", "/api/admin/boards/main/size", strings.Repeat(" ", 16385), "Bearer secret", true, 413},
		{"multiple json values", "PUT", "/api/admin/boards/main/size", `{} {}`, "Bearer secret", true, 400},
		{"get body", "GET", "/api/admin/stats", `{}`, "Bearer secret", false, 400},
		{"image has larger limit", "POST", "/api/admin/boards/main/image", `{"padding":"` + strings.Repeat("a", 20000) + `"}`, "Bearer secret", false, 204},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := secureHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }), "secret", &rateLimiter{}, nil)
			r := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
			if test.chunked {
				r.ContentLength = -1
			}
			r.Header.Set("Authorization", test.header)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != test.status {
				t.Fatalf("status=%d: %s", w.Code, w.Body.String())
			}
			if w.Code == 405 && w.Header().Get("Allow") == "" {
				t.Fatal("missing Allow")
			}
		})
	}
}

type testAccessPolicy struct {
	enabled bool
	admins  map[int64]bool
}

func (p testAccessPolicy) IsTestMode() bool      { return p.enabled }
func (p testAccessPolicy) IsAdmin(id int64) bool { return p.admins[id] }

func TestMaintenanceAccessPolicy(t *testing.T) {
	policy := testAccessPolicy{enabled: true, admins: map[int64]bool{123: true}}
	if !maintenanceAllows("/api/boards/session", 456, policy) {
		t.Fatal("session status must remain available during test mode")
	}
	if maintenanceAllows("/api/boards/main", 456, policy) {
		t.Fatal("non-admin received game data during test mode")
	}
	if !maintenanceAllows("/api/boards/main", 123, policy) {
		t.Fatal("admin was blocked during test mode")
	}
	policy.enabled = false
	if !maintenanceAllows("/api/boards/main", 456, policy) {
		t.Fatal("normal access remained blocked after test mode")
	}
}

func TestRateLimiterBoundedAndExpires(t *testing.T) {
	l := &rateLimiter{}
	now := time.Unix(100, 0)
	for i := 0; i < 10000; i++ {
		if !l.allow(fmt.Sprint(i), 2, now) {
			t.Fatal("unexpected rejection")
		}
	}
	if l.allow("overflow", 2, now) || len(l.entries) != 10000 {
		t.Fatal("table not bounded")
	}
	if !l.allow("0", 2, now) || l.allow("0", 2, now) {
		t.Fatal("wrong request count")
	}
	if !l.allow("overflow", 2, now.Add(time.Minute)) || len(l.entries) != 1 {
		t.Fatal("expired entries not reclaimed")
	}
}

func TestConnectionLimits(t *testing.T) {
	l := &connectionLimits{}
	for i := 0; i < 50; i++ {
		if !l.acquire("same-ip") {
			t.Fatal("early rejection")
		}
	}
	if l.acquire("same-ip") {
		t.Fatal("per-IP limit bypassed")
	}
	for i := 50; i < 2000; i++ {
		if !l.acquire(fmt.Sprint(i)) {
			t.Fatal("early global rejection")
		}
	}
	if l.acquire("overflow") || len(l.byIP) > 2000 {
		t.Fatal("global limit bypassed")
	}
	for i := 0; i < 50; i++ {
		l.release("same-ip")
	}
	for i := 50; i < 2000; i++ {
		l.release(fmt.Sprint(i))
	}
	if l.total != 0 || len(l.byIP) != 0 {
		t.Fatal("connection bookkeeping leaked")
	}
	if !l.acquire("new-ip") {
		t.Fatal("released capacity unavailable")
	}
}

func TestRateLimiterConcurrent(t *testing.T) {
	l := &rateLimiter{}
	now := time.Now()
	var wg sync.WaitGroup
	var mu sync.Mutex
	accepted := 0
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if l.allow("user", 10, now) {
				mu.Lock()
				accepted++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if accepted != 10 {
		t.Fatalf("accepted=%d", accepted)
	}
}

func TestForwardedHeadersNotTrusted(t *testing.T) {
	r := httptest.NewRequest("GET", "/health", nil)
	r.RemoteAddr = "192.0.2.1:1234"
	r.Header.Set("X-Forwarded-For", "198.51.100.1")
	if peerIP(r) != "192.0.2.1" {
		t.Fatal("trusted spoofable forwarding header")
	}
}

func TestOperationIDs(t *testing.T) {
	for _, value := range []string{"", "a:b", "a b", "a\n", strings.Repeat("a", 129), "../id"} {
		if operationIDPattern.MatchString(value) {
			t.Fatalf("accepted %q", value)
		}
	}
	if !operationIDPattern.MatchString("f38c5332-3042-4a25-b4b2-a6dcda42d04e") {
		t.Fatal("client UUID rejected")
	}
	if id() == id() {
		t.Fatal("server IDs collided")
	}
}

func TestPublicBoardDoesNotLeakProfileOrEventMetadata(t *testing.T) {
	author := domain.PixelAuthor{ID: "123", DisplayName: "private-name", Username: "private-username", PhotoURL: "private-photo"}
	event := domain.PixelEvent{Type: "pixel_placed", BoardID: "main", Author: author, EventID: "private-event", OperationID: "private-operation", UserID: "private-user", Version: 1}
	store := state.NewBoardStore()
	store.Apply(event)
	cache := &boardSnapshotCache{}
	raw, _, err := cache.Payload(store, 150, 150)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []any{publicSnapshot(store.Snapshot("main")), eventForClient(event), json.RawMessage(raw)} {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "private-") {
			t.Fatalf("private data leaked: %s", data)
		}
		if !strings.Contains(string(data), "123") {
			t.Fatal("public profile lookup ID missing")
		}
	}
}

func TestTrophyAwardEventOnlyExposesNicknameAndText(t *testing.T) {
	data, err := json.Marshal(trophyAwardEvent{Nickname: "player", Text: "Игроку player выпал трофей!"})
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload) != 2 || payload["nickname"] != "player" || payload["text"] != "Игроку player выпал трофей!" {
		t.Fatalf("unexpected public trophy payload: %s", data)
	}
	for _, forbidden := range []string{"eventId", "userId", "trophyId", "trophyName", "completed"} {
		if _, exists := payload[forbidden]; exists {
			t.Fatalf("private trophy field %q leaked: %s", forbidden, data)
		}
	}
}

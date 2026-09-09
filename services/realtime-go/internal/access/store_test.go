package access

import (
	"context"
	"github.com/redis/go-redis/v9"
	"net"
	"testing"
	"time"
)

type policyHook struct{ bypass []string }

func (h *policyHook) DialHook(next redis.DialHook) redis.DialHook {
	return func(ctx context.Context, network, addr string) (net.Conn, error) { return next(ctx, network, addr) }
}
func (h *policyHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}
func (h *policyHook) ProcessHook(_ redis.ProcessHook) redis.ProcessHook {
	return func(_ context.Context, cmd redis.Cmder) error {
		switch c := cmd.(type) {
		case *redis.StatusCmd:
			c.SetVal("PONG")
		case *redis.StringSliceCmd:
			c.SetVal(h.bypass)
		case *redis.MapStringStringCmd:
			c.SetVal(map[string]string{"123": "9"})
		case *redis.StringCmd:
			c.SetErr(redis.Nil)
			return redis.Nil
		}
		return nil
	}
}

func TestRedisPolicyRemovalSurvivesRefresh(t *testing.T) {
	s, err := New(context.Background(), "", []int64{123})
	if err != nil {
		t.Fatal(err)
	}
	s.client = redis.NewClient(&redis.Options{Addr: "unused:6379"})
	defer s.Close()
	hook := &policyHook{bypass: []string{"123"}}
	s.client.AddHook(hook)
	if err := s.refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s.CooldownFor(123, time.Second) != 0 {
		t.Fatal("Redis bypass ignored")
	}
	hook.bypass = nil
	for i := 0; i < 2; i++ {
		if err := s.refresh(context.Background()); err != nil {
			t.Fatal(err)
		}
		if s.IsCooldownBypassed(123) || s.CooldownFor(123, time.Second) != 9*time.Second {
			t.Fatal("configured admin overrides bot policy")
		}
	}
	hook.bypass = []string{"123"}
	if err := s.refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !s.IsCooldownBypassed(123) {
		t.Fatal("bot re-enable ignored")
	}
}

func TestAdminConfiguration(t *testing.T) {
	for _, raw := range []string{"1,", "-1", "0", "abc", "1 2"} {
		if _, err := ParseAdminIDs(raw); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
	ids, err := ParseAdminIDs("123, 456")
	if err != nil || len(ids) != 2 {
		t.Fatal(ids, err)
	}
	s, err := New(context.Background(), "", ids)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s.CooldownFor(123, time.Second) != 0 || s.CooldownFor(743086174, time.Second) != time.Second {
		t.Fatal("unexpected implicit privilege")
	}
	if !s.IsAdmin(123) || s.IsAdmin(743086174) {
		t.Fatal("configured admin identity was not preserved")
	}
	if s.IsTestMode() {
		t.Fatal("test mode enabled by default")
	}
	if err := s.SetTestMode(context.Background(), true); err != nil || !s.IsTestMode() {
		t.Fatal("test mode was not enabled")
	}
	if err := s.SetTestMode(context.Background(), false); err != nil || s.IsTestMode() {
		t.Fatal("test mode was not disabled")
	}
	if _, err := New(context.Background(), ":bad", nil); err == nil {
		t.Fatal("invalid Redis configuration silently accepted")
	}
}

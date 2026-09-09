package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

func signedData(values url.Values) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		if key != "hash" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+values.Get(key))
	}
	secret := hmac.New(sha256.New, []byte("WebAppData"))
	secret.Write([]byte("test-token"))
	mac := hmac.New(sha256.New, secret.Sum(nil))
	mac.Write([]byte(strings.Join(parts, "\n")))
	values.Set("hash", hex.EncodeToString(mac.Sum(nil)))
	return values.Encode()
}

func TestTelegramValidation(t *testing.T) {
	t.Setenv("TELEGRAM_BOT_TOKEN", "test-token")
	t.Setenv("TELEGRAM_INIT_DATA_MAX_AGE", "3600")
	now := time.Now().Unix()
	base := func() url.Values {
		return url.Values{"auth_date": {strconv.FormatInt(now, 10)}, "user": {`{"id":123,"first_name":"Public"}`}, "query_id": {"test"}, "signature": {"telegram-signature"}}
	}
	valid := signedData(base())
	user, err := ValidateTelegramInitData(valid)
	if err != nil || user.ID != 123 || user.ExpiresAt.Unix() != now+3600 {
		t.Fatalf("valid data rejected: %+v %v", user, err)
	}
	for _, raw := range []string{
		valid + "&user=%7B%22id%22%3A999%7D", valid + "&hash=bad", valid + "&auth_date=1", valid + "&%75ser=x",
		valid + "&query_id=test", valid + "&signature=other", strings.Replace(valid, "telegram-signature", "tampered", 1),
		valid + "&bad=%ZZ", strings.Repeat("x", 16385),
	} {
		if _, err := ValidateTelegramInitData(raw); err == nil {
			t.Fatal("invalid/ambiguous data accepted")
		}
	}
	for _, date := range []int64{now - 3600, now + 120, 0, -1, 9223372036854775807} {
		v := base()
		v.Set("auth_date", strconv.FormatInt(date, 10))
		if _, err := ValidateTelegramInitData(signedData(v)); err == nil {
			t.Fatalf("accepted auth_date=%d", date)
		}
	}
	for _, id := range []string{"0", "-1"} {
		v := base()
		v.Set("user", `{"id":`+id+`}`)
		if _, err := ValidateTelegramInitData(signedData(v)); err == nil {
			t.Fatal("nonpositive user accepted")
		}
	}
	v := base()
	v.Set("user", `{"id":123,"id":456}`)
	if _, err := ValidateTelegramInitData(signedData(v)); err == nil {
		t.Fatal("duplicate JSON identity accepted")
	}
	v = base()
	v.Set("query_id", "test\nauth_date=1")
	if _, err := ValidateTelegramInitData(signedData(v)); err == nil {
		t.Fatal("newline accepted")
	}
	for _, age := range []string{"0", "-1", "invalid", "9223372036854775807"} {
		t.Setenv("TELEGRAM_INIT_DATA_MAX_AGE", age)
		if _, err := ValidateTelegramInitData(valid); err == nil {
			t.Fatal("invalid max age accepted")
		}
	}
	t.Setenv("TELEGRAM_INIT_DATA_MAX_AGE", "3600")
	t.Setenv("TELEGRAM_BOT_TOKEN", "")
	if _, err := ValidateTelegramInitData(valid); err == nil {
		t.Fatal("missing bot token accepted")
	}
}

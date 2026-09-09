package auth

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

var ErrInvalidTelegramData = errors.New("invalid telegram init data")

type TelegramUser struct {
	ID        int64     `json:"id"`
	Username  string    `json:"username"`
	FirstName string    `json:"first_name"`
	LastName  string    `json:"last_name"`
	PhotoURL  string    `json:"photo_url"`
	ExpiresAt time.Time `json:"-"`
}

func ValidateTelegramInitData(raw string) (TelegramUser, error) {
	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	if token == "" || len(raw) == 0 || len(raw) > 16<<10 {
		return TelegramUser{}, ErrInvalidTelegramData
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return TelegramUser{}, ErrInvalidTelegramData
	}
	received := values.Get("hash")
	if len(received) != 64 {
		return TelegramUser{}, ErrInvalidTelegramData
	}
	for key, entries := range values {
		if key == "" || len(entries) != 1 || strings.ContainsAny(key, "=\r\n") || strings.ContainsAny(entries[0], "\r\n") {
			return TelegramUser{}, ErrInvalidTelegramData
		}
	}
	values.Del("hash")
	// Bot-token HMAC covers every remaining field, including signature when
	// supplied. Only the separate Ed25519 verification scheme excludes it.
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+values.Get(key))
	}
	check := strings.Join(parts, "\n")
	secretMAC := hmac.New(sha256.New, []byte("WebAppData"))
	_, _ = secretMAC.Write([]byte(token))
	secret := secretMAC.Sum(nil)
	dataMAC := hmac.New(sha256.New, secret)
	_, _ = dataMAC.Write([]byte(check))
	expected := dataMAC.Sum(nil)
	actual, err := hexDecode(received)
	if err != nil || len(actual) != len(expected) || subtle.ConstantTimeCompare(actual, expected) != 1 {
		return TelegramUser{}, ErrInvalidTelegramData
	}
	authDate, err := strconv.ParseInt(values.Get("auth_date"), 10, 64)
	maxAge, ageErr := strconv.ParseInt(env("TELEGRAM_INIT_DATA_MAX_AGE", "86400"), 10, 64)
	now := time.Now()
	if err != nil || ageErr != nil || maxAge <= 0 || maxAge > 7*86400 || authDate <= 0 || authDate > now.Unix()+30 || authDate <= now.Unix()-maxAge {
		return TelegramUser{}, ErrInvalidTelegramData
	}
	var user TelegramUser
	userJSON := []byte(values.Get("user"))
	// encoding/json accepts duplicate object keys; reject ambiguous identities.
	decoder := json.NewDecoder(bytes.NewReader(userJSON))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return TelegramUser{}, ErrInvalidTelegramData
	}
	seen := make(map[string]bool)
	for decoder.More() {
		key, err := decoder.Token()
		name, ok := key.(string)
		if err != nil || !ok || seen[name] {
			return TelegramUser{}, ErrInvalidTelegramData
		}
		seen[name] = true
		var value json.RawMessage
		if decoder.Decode(&value) != nil {
			return TelegramUser{}, ErrInvalidTelegramData
		}
	}
	if json.Unmarshal(userJSON, &user) != nil || user.ID <= 0 {
		return TelegramUser{}, ErrInvalidTelegramData
	}
	user.ExpiresAt = time.Unix(authDate, 0).Add(time.Duration(maxAge) * time.Second)
	return user, nil
}

func hexDecode(value string) ([]byte, error) {
	decoded := make([]byte, len(value)/2)
	for i := range decoded {
		var high, low byte
		if !hexNibble(value[i*2], &high) || !hexNibble(value[i*2+1], &low) {
			return nil, ErrInvalidTelegramData
		}
		decoded[i] = high<<4 | low
	}
	if len(value)%2 != 0 {
		return nil, ErrInvalidTelegramData
	}
	return decoded, nil
}

func hexNibble(char byte, output *byte) bool {
	switch {
	case char >= '0' && char <= '9':
		*output = char - '0'
	case char >= 'a' && char <= 'f':
		*output = char - 'a' + 10
	case char >= 'A' && char <= 'F':
		*output = char - 'A' + 10
	default:
		return false
	}
	return true
}
func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

package auth

import (
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
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	PhotoURL  string `json:"photo_url"`
}

func ValidateTelegramInitData(raw string) (TelegramUser, error) {
	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	if token == "" {
		return TelegramUser{}, ErrInvalidTelegramData
	}
	values, err := url.ParseQuery(raw)
	if err != nil {
		return TelegramUser{}, ErrInvalidTelegramData
	}
	received := values.Get("hash")
	if received == "" {
		return TelegramUser{}, ErrInvalidTelegramData
	}
	values.Del("hash")
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
	maxAge, _ := strconv.ParseInt(env("TELEGRAM_INIT_DATA_MAX_AGE", "86400"), 10, 64)
	if err != nil || authDate == 0 || time.Since(time.Unix(authDate, 0)).Abs() > time.Duration(maxAge)*time.Second {
		return TelegramUser{}, ErrInvalidTelegramData
	}
	var user TelegramUser
	if json.Unmarshal([]byte(values.Get("user")), &user) != nil || user.ID == 0 {
		return TelegramUser{}, ErrInvalidTelegramData
	}
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

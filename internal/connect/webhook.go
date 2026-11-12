package connect

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var ErrSignature = errors.New("GRAB-E317: webhook verification failed")
var eventID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
var unixSeconds = regexp.MustCompile(`^(0|[1-9][0-9]{0,11})$`)
var signature = regexp.MustCompile(`^v1=[0-9a-f]{64}$`)

func Sign(id, stamp string, body, key []byte) string {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(id + "." + stamp + "."))
	m.Write(body)
	return "v1=" + hex.EncodeToString(m.Sum(nil))
}
func Verify(h http.Header, body []byte, keys [][]byte, now time.Time) (string, error) {
	if len(body) > 1<<20 || len(keys) < 1 || len(keys) > 2 || h.Get("Content-Encoding") != "" {
		return "", ErrSignature
	}
	one := func(n string) (string, bool) { v := h.Values(n); return strings.Join(v, ""), len(v) == 1 }
	id, ok := one("Grab-Event-Id")
	if !ok || !eventID.MatchString(id) {
		return "", ErrSignature
	}
	stamp, ok := one("Grab-Event-Time")
	if !ok || !unixSeconds.MatchString(stamp) {
		return "", ErrSignature
	}
	s, ok := one("Grab-Signature")
	if !ok || !signature.MatchString(s) {
		return "", ErrSignature
	}
	seconds, e := strconv.ParseInt(stamp, 10, 64)
	if e != nil || seconds < now.Unix()-300 || seconds > now.Unix()+300 {
		return "", ErrSignature
	}
	provided, _ := hex.DecodeString(s[3:])
	match := false
	for _, key := range keys {
		if len(key) < 16 || len(key) > 8192 {
			return "", ErrSignature
		}
		expected, _ := hex.DecodeString(Sign(id, stamp, body, key)[3:])
		match = hmac.Equal(provided, expected) || match
	}
	if !match {
		return "", ErrSignature
	}
	return id, nil
}

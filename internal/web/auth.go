package web

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"log"
	"net/url"
	"sort"
	"strings"
	"time"
)

// ValidateInitData validates Telegram WebApp initData using HMAC-SHA256.
// Returns the user ID on success, or 0 on failure.
func ValidateInitData(initData, botToken string) (int64, bool) {
	if initData == "" || botToken == "" {
		log.Printf("[AUTH-VALIDATE] empty initData or botToken")
		return 0, false
	}

	// Parse query string
	params, err := url.ParseQuery(initData)
	if err != nil {
		log.Printf("[AUTH-VALIDATE] parseQuery error: %v", err)
		return 0, false
	}

	// Extract hash
	hash := params.Get("hash")
	if hash == "" {
		log.Printf("[AUTH-VALIDATE] no hash in params, keys: %v", func() []string {
			keys := make([]string, 0, len(params))
			for k := range params { keys = append(keys, k) }
			return keys
		}())
		return 0, false
	}

	// Check auth_date (must be within 24 hours)
	authDateStr := params.Get("auth_date")
	if authDateStr == "" {
		return 0, false
	}
	authDateInt := int64(0)
	for _, c := range authDateStr {
		if c >= '0' && c <= '9' {
			authDateInt = authDateInt*10 + int64(c-'0')
		} else {
			break
		}
	}
	if time.Now().Unix()-authDateInt > 86400 {
		log.Printf("[AUTH-VALIDATE] auth_date too old: auth_date=%d now=%d diff=%d", authDateInt, time.Now().Unix(), time.Now().Unix()-authDateInt)
		return 0, false
	}
	log.Printf("[AUTH-VALIDATE] auth_date OK: age=%ds", time.Now().Unix()-authDateInt)

	// Build data-check-string: sort all keys except "hash", join as key=value\n
	delete(params, "hash")
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	for i, k := range keys {
		if i > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(k)
		sb.WriteByte('=')
		sb.WriteString(params.Get(k))
	}
	dataCheckString := sb.String()

	// Compute secret_key = HMAC_SHA256(key=botToken, data="WebAppData")
	secretKeyMac := hmac.New(sha256.New, []byte(botToken))
	secretKeyMac.Write([]byte("WebAppData"))
	secretKey := secretKeyMac.Sum(nil)

	// Compute hash = HMAC_SHA256(key=secretKey, data=dataCheckString)
	hashMac := hmac.New(sha256.New, secretKey)
	hashMac.Write([]byte(dataCheckString))
	computedHash := hashMac.Sum(nil)

	// Compare
	computedHex := hex.EncodeToString(computedHash)
	log.Printf("[AUTH-VALIDATE] hash compare: computed=%s received=%s match=%v", computedHex[:16]+"...", hash[:min(16, len(hash))]+"...", hmac.Equal([]byte(computedHex), []byte(hash)))
	if !hmac.Equal([]byte(computedHex), []byte(hash)) {
		log.Printf("[AUTH-VALIDATE] HASH MISMATCH")
		return 0, false
	}

	// Extract user ID from user JSON
	userJSON := params.Get("user")
	if userJSON == "" {
		return 0, false
	}

	userID := extractUserID(userJSON)
	return userID, userID > 0
}

// extractUserID parses {"id":12345,...} to extract the user ID.
func extractUserID(jsonStr string) int64 {
	// Simple JSON parsing for {"id":12345,...}
	s := jsonStr
	// Find "id":
	idx := strings.Index(s, `"id":`)
	if idx < 0 {
		return 0
	}
	s = s[idx+5:]
	// Skip whitespace
	for len(s) > 0 && s[0] == ' ' {
		s = s[1:]
	}
	// Parse number
	result := int64(0)
	for len(s) > 0 && s[0] >= '0' && s[0] <= '9' {
		result = result*10 + int64(s[0]-'0')
		s = s[1:]
	}
	return result
}

func min(a, b int) int {
	if a < b { return a }
	return b
}



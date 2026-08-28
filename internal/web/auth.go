package web

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/url"
	"sort"
	"strconv"
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

	// Parse initData — url.ParseQuery handles URL decoding
	params, err := url.ParseQuery(initData)
	if err != nil {
		log.Printf("[AUTH-VALIDATE] parseQuery error: %v", err)
		return 0, false
	}

	// Debug: log what we received (for troubleshooting)
	if userVal := params.Get("user"); userVal != "" {
		log.Printf("[AUTH-VALIDATE] user field length: %d, starts with: %s", len(userVal), userVal[:min(20, len(userVal))])
	}

	hash := params.Get("hash")
	if hash == "" {
		log.Printf("[AUTH-VALIDATE] no hash in params, keys: %v", func() []string {
			keys := make([]string, 0, len(params))
			for k := range params {
				keys = append(keys, k)
			}
			return keys
		}())
		return 0, false
	}

	authDateStr := params.Get("auth_date")
	if authDateStr == "" {
		return 0, false
	}
	authDateInt, err := strconv.ParseInt(authDateStr, 10, 64)
	if err != nil {
		log.Printf("[AUTH-VALIDATE] invalid auth_date: %v", err)
		return 0, false
	}
	if time.Now().Unix()-authDateInt > 86400 {
		log.Printf("[AUTH-VALIDATE] auth_date too old: auth_date=%d now=%d diff=%d", authDateInt, time.Now().Unix(), time.Now().Unix()-authDateInt)
		return 0, false
	}
	log.Printf("[AUTH-VALIDATE] auth_date OK: age=%ds", time.Now().Unix()-authDateInt)

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
		// Use the raw value from params — url.ParseQuery already decoded it
		// Telegram signs the decoded form, so this should match
		sb.WriteString(params.Get(k))
	}
	dataCheckString := sb.String()
	log.Printf("[AUTH-VALIDATE] data-check-string length: %d", len(dataCheckString))

	secretKeyMac := hmac.New(sha256.New, []byte(botToken))
	secretKeyMac.Write([]byte("WebAppData"))
	secretKey := secretKeyMac.Sum(nil)

	hashMac := hmac.New(sha256.New, secretKey)
	hashMac.Write([]byte(dataCheckString))
	computedHash := hashMac.Sum(nil)

	computedHex := hex.EncodeToString(computedHash)
	log.Printf("[AUTH-VALIDATE] hash compare: computed=%s received=%s match=%v", computedHex[:16]+"...", hash[:min(16, len(hash))]+"...", hmac.Equal([]byte(computedHex), []byte(hash)))
	if !hmac.Equal([]byte(computedHex), []byte(hash)) {
		log.Printf("[AUTH-VALIDATE] HASH MISMATCH")
		return 0, false
	}

	userJSON := params.Get("user")
	if userJSON == "" {
		return 0, false
	}

	userID := extractUserID(userJSON)
	return userID, userID > 0
}

func extractUserID(jsonStr string) int64 {
	var user struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &user); err != nil {
		log.Printf("[AUTH-VALIDATE] failed to parse user JSON: %v", err)
		return 0
	}
	return user.ID
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

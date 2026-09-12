package web

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"info-bot-go/internal/applogin"
)

// handleAppAuth обмінює одноразовий код з бота (/login) на підписаний
// initData. Підпис робить сервер тим самим HMAC-ключем, що й Telegram,
// тому всі інші ендпоінти приймають цей initData без жодних змін.
func (s *Server) handleAppAuth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, APIResponse{OK: false, Err: "method not allowed"})
		return
	}
	if s.appLogin == nil {
		writeJSON(w, http.StatusServiceUnavailable, APIResponse{OK: false, Err: "app login disabled"})
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{OK: false, Err: "invalid json"})
		return
	}
	userID, ok := s.appLogin.Exchange(req.Code)
	if !ok {
		log.Printf("[APP-AUTH] invalid code: remote=%s", r.RemoteAddr)
		writeJSON(w, http.StatusUnauthorized, APIResponse{OK: false, Err: "невірний або прострочений код"})
		return
	}
	initData, err := generateInitData(userID, s.cfg.BotToken)
	if err != nil {
		log.Printf("[APP-AUTH] generate initData: %v", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{OK: false, Err: "internal error"})
		return
	}
	log.Printf("[APP-AUTH] user=%d received app initData", userID)
	writeJSON(w, http.StatusOK, APIResponse{OK: true, Data: map[string]any{
		"init_data":  initData,
		"expires_in": 86400,
	}})
}

// generateInitData будує initData так, як його сформував би Telegram:
// auth_date + user + HMAC-підпис із секретом HMAC("WebAppData", bot_token).
// Сервер перевіряє підпис тим самим кодом (computeInitDataHash), тому
// згенерований initData проходить authMiddleware без змін.
func generateInitData(userID int64, botToken string) (string, error) {
	if botToken == "" {
		return "", fmt.Errorf("empty bot token")
	}
	userJSON := fmt.Sprintf(
		`{"id":%d,"first_name":"Застосунок","last_name":"","username":"","language_code":"uk","is_premium":false,"allows_write_to_pm":false}`,
		userID,
	)
	authDate := strconv.FormatInt(time.Now().Unix(), 10)
	// data_check_string: відсортовані ключі зі значеннями через \n
	dataCheck := "auth_date=" + authDate + "\nuser=" + userJSON
	hashHex := hex.EncodeToString(computeInitDataHash(dataCheck, botToken))
	return "auth_date=" + authDate + "&user=" + url.QueryEscape(userJSON) + "&hash=" + hashHex, nil
}

// Ensure applogin import is used by the Server struct (see server.go).
var _ = applogin.New

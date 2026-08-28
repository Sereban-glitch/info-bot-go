package dostup

// Клиент временной почты mail.tm (https://docs.mail.tm/) — используется для
// автоматической регистрации аккаунтов на dostup.org.ua.
//
// mail.tm принимает письма, но НЕ отправляет их (POST /messages = 405) —
// это идеально для нашей задачи: подтверждение регистрации приходит СЮДА.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

const MailTMAPI = "https://api.mail.tm"

var (
	ErrMailboxExists  = errors.New("mail.tm: ящик с таким адресом уже существует")
	ErrNoConfirmEmail = errors.New("mail.tm: письмо с подтверждением не пришло за отведённое время")
)

// MailAccount — учётные данные временного ящика.
type MailAccount struct {
	Address  string `json:"address"`
	Password string `json:"password"`
	Token    string `json:"token"` // Bearer-токен API
}

// MailMessage — краткое описание письма.
type MailMessage struct {
	ID      string `json:"id"`
	From    string `json:"from"`
	Subject string `json:"subject"`
	Text    string `json:"text"`
	Intro   string `json:"intro"`
}

type mailTMClient struct {
	http *http.Client
}

func newMailTM() *mailTMClient {
	return &mailTMClient{http: &http.Client{Timeout: 30 * time.Second}}
}

func (m *mailTMClient) request(method, path string, body interface{}, token string) ([]byte, int, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, MailTMAPI+path, rd)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := m.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	return b, resp.StatusCode, err
}

// CreateMailbox создаёт новый ящик mail.tm и возвращает учётные данные + API-токен.
// Пароль рекомендуется >= 12 символов с цифрами и спецсимволами.
func CreateMailbox(localPart, password string) (*MailAccount, error) {
	m := newMailTM()

	// 1) доступные домены
	b, code, err := m.request("GET", "/domains?page=1", nil, "")
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("mail.tm: /domains: HTTP %d", code)
	}
	var domainsResp struct {
		Member []struct {
			Domain   string `json:"domain"`
			IsActive bool   `json:"isActive"`
		} `json:"hydra:member"`
	}
	if err := json.Unmarshal(b, &domainsResp); err != nil {
		return nil, err
	}
	var domain string
	for _, d := range domainsResp.Member {
		if d.IsActive {
			domain = d.Domain
			break
		}
	}
	if domain == "" {
		return nil, errors.New("mail.tm: нет активных доменов")
	}

	address := localPart + "@" + domain

	// 2) создание аккаунта
	payload := map[string]string{"address": address, "password": password}
	b, code, err = m.request("POST", "/accounts", payload, "")
	if err != nil {
		return nil, err
	}
	if code == 422 {
		return nil, ErrMailboxExists
	}
	if code != 201 {
		return nil, fmt.Errorf("mail.tm: /accounts: HTTP %d: %s", code, string(b[:min(200, len(b))]))
	}

	// 3) токен
	return loginMailbox(m, address, password)
}

func loginMailbox(m *mailTMClient, address, password string) (*MailAccount, error) {
	payload := map[string]string{"address": address, "password": password}
	b, code, err := m.request("POST", "/token", payload, "")
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("mail.tm: /token: HTTP %d", code)
	}
	var tok struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(b, &tok); err != nil {
		return nil, err
	}
	if tok.Token == "" {
		return nil, errors.New("mail.tm: пустой токен")
	}
	return &MailAccount{Address: address, Password: password, Token: tok.Token}, nil
}

// LoginMailbox возвращает API-токен для существующего ящика.
func LoginMailbox(address, password string) (*MailAccount, error) {
	return loginMailbox(newMailTM(), address, password)
}

// ListMessages возвращает входящие письма (свежие первыми).
func (a *MailAccount) ListMessages() ([]MailMessage, error) {
	m := newMailTM()
	b, code, err := m.request("GET", "/messages?page=1", nil, a.Token)
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("mail.tm: /messages: HTTP %d", code)
	}
	var resp struct {
		Member []struct {
			ID   string `json:"id"`
			From struct {
				Address string `json:"address"`
			} `json:"from"`
			Subject string `json:"subject"`
			Intro   string `json:"intro"`
		} `json:"hydra:member"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		return nil, err
	}
	var out []MailMessage
	for _, mm := range resp.Member {
		out = append(out, MailMessage{ID: mm.ID, From: mm.From.Address, Subject: mm.Subject, Intro: mm.Intro})
	}
	return out, nil
}

// GetMessage возвращает полное письмо (поле Text).
func (a *MailAccount) GetMessage(id string) (*MailMessage, error) {
	m := newMailTM()
	b, code, err := m.request("GET", "/messages/"+id, nil, a.Token)
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("mail.tm: /messages/%s: HTTP %d", id, code)
	}
	var msg struct {
		ID   string `json:"id"`
		From struct {
			Address string `json:"address"`
		} `json:"from"`
		Subject string `json:"subject"`
		Text    string `json:"text"`
	}
	if err := json.Unmarshal(b, &msg); err != nil {
		return nil, err
	}
	return &MailMessage{ID: msg.ID, From: msg.From.Address, Subject: msg.Subject, Text: msg.Text}, nil
}

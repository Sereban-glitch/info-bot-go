package stars

// Клиент Bot API для создания ссылок на оплату Telegram Stars.
// Прямой HTTP (а не telebot) — чтобы легко подменять адрес в тестах
// и не тянуть зависимости между пакетами.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// DefaultAPIBase — адрес Bot API.
const DefaultAPIBase = "https://api.telegram.org"

// Client создаёт инвойс-ссылки через Bot API.
type Client struct {
	http     *http.Client
	botToken string
	apiBase  string
}

// NewClient — клиент для бота с данным токеном.
func NewClient(botToken string) *Client {
	return &Client{
		http:     &http.Client{Timeout: 30 * time.Second},
		botToken: botToken,
		apiBase:  DefaultAPIBase,
	}
}

// SetAPIBase подменяет адрес Bot API (для тестов).
func (c *Client) SetAPIBase(base string) { c.apiBase = base }

type invoiceRequest struct {
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Payload     string  `json:"payload"`
	Currency    string  `json:"currency"` // XTR = Telegram Stars
	Prices      []price `json:"prices"`
}

type price struct {
	Label  string `json:"label"`
	Amount int    `json:"amount"` // целые Stars
}

// CreateInvoiceLink создаёт ссылку на оплату Stars.
// provider_token для XTR не нужен — это внутренняя валюта Telegram.
func (c *Client) CreateInvoiceLink(title, description, payload string, priceXTR int) (string, error) {
	if c.botToken == "" {
		return "", fmt.Errorf("stars: пустой токен бота")
	}
	if priceXTR <= 0 {
		return "", fmt.Errorf("stars: цена должна быть больше нуля")
	}
	body, err := json.Marshal(invoiceRequest{
		Title:       title,
		Description: description,
		Payload:     payload,
		Currency:    "XTR",
		Prices:      []price{{Label: title, Amount: priceXTR}},
	})
	if err != nil {
		return "", err
	}
	url := fmt.Sprintf("%s/bot%s/createInvoiceLink", c.apiBase, c.botToken)
	resp, err := c.http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("stars: createInvoiceLink: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("stars: чтение ответа: %w", err)
	}
	var parsed struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
		Result      string `json:"result"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", fmt.Errorf("stars: некорректный ответ Bot API: %w", err)
	}
	if !parsed.OK {
		return "", fmt.Errorf("stars: Bot API отклонил инвойс: %s", parsed.Description)
	}
	if parsed.Result == "" {
		return "", fmt.Errorf("stars: Bot API вернул пустую ссылку")
	}
	return parsed.Result, nil
}

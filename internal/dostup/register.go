package dostup

// Полный цикл регистрации нового аккаунта на dostup.org.ua:
//
//	1. Создать временный ящик mail.tm (CreateMailbox)
//	2. POST /profile/sign_up — форма регистрации (signup_token из страницы входа)
//	3. Ждать письмо «Підтвердіть ваш акаунт» (poll mail.tm)
//	4. Перейти по ссылке https://dostup.org.ua/c/<код> — активация + автовход
//
// Открытые при разведке требования к регистрации:
//   - пароль >= 12 символов (иначе «Пароль закороткий»)
//   - чекбокс name_public_ok=1 обязателен (публичность имени на портале)
//   - reCAPTCHA есть только в браузере; сервер POST без капчи принимает

import (
	"errors"
	"fmt"
	"math/rand"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var reSignupToken = regexp.MustCompile(`name="token" id="signup_token" value="([^"]+)"`)
var reConfirmLink = regexp.MustCompile(`https://dostup\.org\.ua/c/[A-Za-z0-9_-]+`)

// RegisterResult — итог регистрации: готовый к работе клиент + учётки.
type RegisterResult struct {
	Client   *Client      // клиент с активной сессией
	Account  *MailAccount // ящик mail.tm (сюда придут ответы портала)
	SitePass string       // пароль на dostup.org.ua
}

// RegisterFullFlow регистрирует новый аккаунт и возвращает залогиненного клиента.
//
// name — отображаемое имя (напр. «Сергій Акімов»); localPart — логин ящика
// (только латиница/цифры, без @); sitePassword — пароль сайта (>= 12 символов!);
// waitTimeout — сколько ждать письмо-подтверждение (рекомендуется 3-5 минут).
func RegisterFullFlow(sessionFile, name, localPart, sitePassword string, waitTimeout time.Duration) (*RegisterResult, error) {
	if len(sitePassword) < 12 {
		return nil, errors.New("dostup: пароль сайта должен быть >= 12 символов")
	}
	if localPart == "" {
		localPart = fmt.Sprintf("user%d%d", time.Now().Unix(), rand.Intn(9999))
	}

	// 1) временный ящик
	mailPass := fmt.Sprintf("Dp%s!%d", randomString(6), rand.Intn(9999))
	mail, err := CreateMailbox(localPart, mailPass)
	if err != nil {
		return nil, fmt.Errorf("mail.tm: %w", err)
	}

	// 2) страница входа → signup_token
	c := New(sessionFile)
	page, code, err := c.get("/profile/sign_in?r=%2F")
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("dostup: страница регистрации: HTTP %d", code)
	}
	m := reSignupToken.FindStringSubmatch(page)
	if m == nil {
		return nil, ErrTokenNotFound
	}

	// 3) отправка формы регистрации
	form := url.Values{
		"user_signup[email]":                 {mail.Address},
		"user_signup[name]":                  {name},
		"name_public_ok":                     {"1"},
		"user_signup[password]":              {sitePassword},
		"user_signup[password_confirmation]": {sitePassword},
		"token":                              {m[1]},
	}
	resp, code, err := c.post("/profile/sign_up", form)
	if err != nil {
		return nil, err
	}
	if code == 200 && strings.Contains(resp, "errorExplanation") {
		return nil, fmt.Errorf("dostup: ошибка валидации регистрации: %s", extractError(resp))
	}
	if !strings.Contains(resp, "перевірте вашу пошту") && !strings.Contains(resp, "Тепер перевірте") {
		// Некоторые ответы — 302; считаем успешным только явное сообщение
		if code != 302 && code != 303 {
			return nil, fmt.Errorf("dostup: неожиданный ответ регистрации (HTTP %d)", code)
		}
	}

	// 4) ждём письмо с подтверждением
	deadline := time.Now().Add(waitTimeout)
	var confirmURL string
	for time.Now().Before(deadline) {
		time.Sleep(10 * time.Second)
		msgs, err := mail.ListMessages()
		if err != nil {
			continue
		}
		for _, msg := range msgs {
			if strings.Contains(msg.Subject, "Підтвердіть") || strings.Contains(msg.From, "dostup.org.ua") {
				full, err := mail.GetMessage(msg.ID)
				if err == nil && full != nil {
					if link := reConfirmLink.FindString(full.Text); link != "" {
						confirmURL = link
						break
					}
				}
			}
		}
		if confirmURL != "" {
			break
		}
	}
	if confirmURL == "" {
		return nil, ErrNoConfirmEmail
	}

	// 5) переход по ссылке подтверждения (= активация + автовход)
	path := strings.TrimPrefix(confirmURL, BaseURL)
	_, code, err = c.get(path)
	if err != nil {
		return nil, fmt.Errorf("dostup: активация: %w", err)
	}
	if !c.IsLoggedIn() {
		// иногда нужна явная сессия после активации
		if err := c.Login(mail.Address, sitePassword); err != nil {
			return nil, fmt.Errorf("dostup: активирован, но вход не удался: %w", err)
		}
	}

	return &RegisterResult{Client: c, Account: mail, SitePass: sitePassword}, nil
}

// extractError вытаскивает текст блока errorExplanation из HTML.
func extractError(html string) string {
	re := regexp.MustCompile(`errorExplanation[^>]*>(.*?)</div>`)
	m := re.FindStringSubmatch(html)
	if m == nil {
		return "неизвестная ошибка"
	}
	text := regexp.MustCompile(`<[^>]+>`).ReplaceAllString(m[1], " ")
	return strings.Join(strings.Fields(text), " ")
}

const letters = "abcdefghijklmnopqrstuvwxyz"

func randomString(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

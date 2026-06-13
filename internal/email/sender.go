package email

import (
	"crypto/rand"
	"fmt"
	"net/smtp"
	"strings"
	"time"

	"info-bot-go/internal/config"
	"info-bot-go/internal/session"
)

// Sender handles email sending via SMTP.
// Supports any SMTP server: Gmail, Resend, Brevo, self-hosted Postfix, etc.
// Configuration is loaded from environment variables via config.Config.
type Sender struct {
	cfg *config.Config
}

// NewSender creates a new email sender.
func NewSender(cfg *config.Config) *Sender {
	return &Sender{cfg: cfg}
}

// generateMessageID creates a proper unique Message-ID per RFC 2822.
// Uses the configured SMTP host domain for Message-ID alignment.
func generateMessageID(smtpHost string) string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	// Extract domain from SMTP host (e.g., "smtp.gmail.com" -> "gmail.com")
	domain := smtpHost
	if parts := strings.Split(smtpHost, "."); len(parts) > 1 {
		domain = strings.Join(parts[1:], ".")
	}
	return fmt.Sprintf("<%x.%d@%s>", b, time.Now().UnixNano(), domain)
}

// buildCommonHeaders builds headers shared between plain and attachment emails.
func buildCommonHeaders(from, to, replyTo, cc, messageID string) string {
	var msg strings.Builder
	fmt.Fprintf(&msg, "From: %s\r\n", from)
	fmt.Fprintf(&msg, "To: %s\r\n", to)
	if cc != "" {
		fmt.Fprintf(&msg, "Cc: %s\r\n", cc)
	}
	if replyTo != "" {
		fmt.Fprintf(&msg, "Reply-To: %s\r\n", replyTo)
	} else {
		fmt.Fprintf(&msg, "Reply-To: %s\r\n", from)
	}
	fmt.Fprintf(&msg, "Message-ID: %s\r\n", messageID)
	fmt.Fprintf(&msg, "Date: %s\r\n", time.Now().Format("Mon, 02 Jan 2006 15:04:05 -0700"))
	msg.WriteString("X-Mailer: InfoBot-UA/1.0\r\n")
	msg.WriteString("X-Priority: 3 (Normal)\r\n")
	msg.WriteString("Auto-Submitted: no\r\n")
	return msg.String()
}

// Send sends an information request email and returns the real Message-ID.
func (s *Sender) Send(to, subject, body, replyTo, cc string) (string, error) {
	if s.cfg.SMTPUser == "" || s.cfg.SMTPPassword == "" {
		return "", fmt.Errorf("SMTP credentials not configured (SMTP_USER/SMTP_PASSWORD)")
	}

	from := s.cfg.SMTPFromAddr
	messageID := generateMessageID(s.cfg.SMTPHost)

	auth := smtp.PlainAuth("", s.cfg.SMTPUser, s.cfg.SMTPPassword, s.cfg.SMTPHost)

	// Build email message with proper headers
	var msg strings.Builder
	msg.WriteString(buildCommonHeaders(from, to, replyTo, cc, messageID))
	fmt.Fprintf(&msg, "Subject: %s\r\n", subject)
	msg.WriteString("MIME-Version: 1.0\r\n")
	msg.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	msg.WriteString("Content-Transfer-Encoding: 8bit\r\n")
	msg.WriteString("\r\n")
	msg.WriteString(body)

	recipients := []string{to}
	if cc != "" {
		recipients = append(recipients, cc)
	}

	err := smtp.SendMail(s.cfg.SMTPAddr(), auth, from, recipients, []byte(msg.String()))
	if err != nil {
		return "", fmt.Errorf("SMTP send failed [%s]: %w", s.cfg.SMTPAddr(), err)
	}

	return messageID, nil
}

// SendWithAttachment sends an email with a PDF attachment.
func (s *Sender) SendWithAttachment(to, subject, body, replyTo, cc string, attachment []byte, attachmentName string) (string, error) {
	if s.cfg.SMTPUser == "" || s.cfg.SMTPPassword == "" {
		return "", fmt.Errorf("SMTP credentials not configured (SMTP_USER/SMTP_PASSWORD)")
	}

	from := s.cfg.SMTPFromAddr
	messageID := generateMessageID(s.cfg.SMTPHost)

	auth := smtp.PlainAuth("", s.cfg.SMTPUser, s.cfg.SMTPPassword, s.cfg.SMTPHost)

	// Build MIME multipart message
	boundary := fmt.Sprintf("----=_Part_%x", time.Now().UnixNano())

	var msg strings.Builder
	msg.WriteString(buildCommonHeaders(from, to, replyTo, cc, messageID))
	fmt.Fprintf(&msg, "Subject: %s\r\n", subject)
	msg.WriteString("MIME-Version: 1.0\r\n")
	msg.WriteString("Content-Type: multipart/mixed; boundary=\"" + boundary + "\"\r\n")
	msg.WriteString("\r\n")

	// Text part
	msg.WriteString("--" + boundary + "\r\n")
	msg.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	msg.WriteString("Content-Transfer-Encoding: 8bit\r\n")
	msg.WriteString("\r\n")
	msg.WriteString(body + "\r\n")
	msg.WriteString("\r\n")

	// Attachment part (PDF)
	msg.WriteString("--" + boundary + "\r\n")
	msg.WriteString("Content-Type: application/pdf; name=\"" + attachmentName + "\"\r\n")
	msg.WriteString("Content-Transfer-Encoding: base64\r\n")
	msg.WriteString("Content-Disposition: attachment; filename=\"" + attachmentName + "\"\r\n")
	msg.WriteString("\r\n")

	// Base64 encode the attachment, 76 chars per line
	b64 := base64Encode(attachment)
	for i := 0; i < len(b64); i += 76 {
		end := i + 76
		if end > len(b64) {
			end = len(b64)
		}
		msg.WriteString(b64[i:end] + "\r\n")
	}

	msg.WriteString("\r\n--" + boundary + "--\r\n")

	recipients := []string{to}
	if cc != "" {
		recipients = append(recipients, cc)
	}

	err := smtp.SendMail(s.cfg.SMTPAddr(), auth, from, recipients, []byte(msg.String()))
	if err != nil {
		return "", fmt.Errorf("SMTP send with attachment failed [%s]: %w", s.cfg.SMTPAddr(), err)
	}

	return messageID, nil
}

// base64Encode encodes bytes to base64 string.
func base64Encode(data []byte) string {
	const base64Table = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	var result []byte
	for i := 0; i < len(data); i += 3 {
		var b0, b1, b2 byte
		b0 = data[i]
		if i+1 < len(data) {
			b1 = data[i+1]
		}
		if i+2 < len(data) {
			b2 = data[i+2]
		}
		result = append(result, base64Table[b0>>2])
		result = append(result, base64Table[(b0&0x03)<<4|(b1>>4)])
		if i+1 < len(data) {
			result = append(result, base64Table[(b1&0x0f)<<2|(b2>>6)])
		} else {
			result = append(result, '=')
		}
		if i+2 < len(data) {
			result = append(result, base64Table[b2&0x3f])
		} else {
			result = append(result, '=')
		}
	}
	return string(result)
}

// BuildRequestText generates the full text of an information request letter.
func BuildRequestText(data RequestData) string {
	date := time.Now().Format("02.01.2006")

	var contactLines string
	if data.PostalAddress != "" && !data.UseSharedMailbox {
		contactLines = "\nАдреса: " + data.PostalAddress
	}

	var emailLine string
	if data.Email != "" && !data.UseSharedMailbox {
		emailLine = "\nЕлектронна пошта: " + data.Email
	}

	return fmt.Sprintf(`КОМУ: %s
ЗАПИТУВАЧ: %s%s%s

ЗАПИТ НА ОТРИМАННЯ ПУБЛІЧНОЇ ІНФОРМАЦІЇ
(Закон України №2939-VI «Про доступ до публічної інформації»)

Відповідно до статей 1, 13, 19, 20 Закону України «Про доступ до публічної інформації» (№2939-VI від 13.01.2011), прошу надати наступну інформацію:

%s

У разі, якщо Ви не володієте запитуваною інформацією, на підставі ст. 22 Закону прошу направити цей запит належному розпоряднику з одночасним повідомленням про це запитувача.

Відповідь прошу надати у строк, встановлений законом (не пізніше п'яти робочих днів з дня отримання запиту, ст. 20), в електронному вигляді на адресу електронної пошти, з якої надіслано цей запит.

Згідно з частиною 2 статті 19 Закону, цей запит надсилається в електронній формі та не потребує власноручного підпису.

%s

З повагою,
%s`, data.RecipientName, data.FullName, contactLines, emailLine, data.Body, date, data.FullName)
}

// BuildSubject generates the email subject line.
func BuildSubject(subject string) string {
	return "Запит на публічну інформацію (ЗУ №2939-VI) — " + subject
}

// RequestData contains all data needed to build a request letter.
type RequestData struct {
	FullName         string
	PostalAddress    string
	RecipientName    string
	Subject          string
	Body             string
	Email            string
	UseSharedMailbox bool
	SharedMailbox    string
}

// BuildRequestDataFromSession builds RequestData from session state.
func BuildRequestDataFromSession(p session.Profile, d session.Draft, sharedMailbox string) *RequestData {
	useShared := d.UseSharedMailbox || p.Email == ""
	email := p.Email
	if email == "" {
		email = sharedMailbox
	}

	return &RequestData{
		FullName:         session.ProfileDisplayName(p),
		PostalAddress:    p.PostalAddress,
		RecipientName:    d.RecipientName,
		Subject:          d.Subject,
		Body:             d.Body,
		Email:            email,
		UseSharedMailbox: useShared,
		SharedMailbox:    sharedMailbox,
	}
}


package imap

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/emersion/go-imap"
	imapClient "github.com/emersion/go-imap/client"
	"github.com/emersion/go-message/mail"

	tb "gopkg.in/telebot.v3"

	"info-bot-go/internal/sentlog"
	"info-bot-go/internal/stats"
)

// Watcher monitors a Gmail IMAP inbox for replies to sent requests.
type Watcher struct {
	imapHost  string
	imapPort  int
	user      string
	password  string
	interval  time.Duration
	stopCh    chan struct{}
	sentLog   *sentlog.SentLog
	stats     *stats.Stats
	lastScan  time.Time
	lastCount int
	attachDir string
}

// NewWatcher creates a new IMAP watcher.
func NewWatcher(imapHost string, imapPort int, user, password string, pollMinutes int) *Watcher {
	if pollMinutes < 5 {
		pollMinutes = 5
	}
	return &Watcher{
		imapHost:  imapHost,
		imapPort:  imapPort,
		user:      user,
		password:  password,
		interval:  time.Duration(pollMinutes) * time.Minute,
		stopCh:    make(chan struct{}),
		attachDir: ".attachments",
	}
}

// SetSentLog injects the sent log for reply matching.
func (w *Watcher) SetSentLog(sl *sentlog.SentLog) {
	w.sentLog = sl
}

// SetStats injects the global stats counter for reply/bounce tracking.
func (w *Watcher) SetStats(s *stats.Stats) {
	w.stats = s
}

// Status returns the current watcher status.
func (w *Watcher) Status() (enabled bool, intervalMin int, lastScan string, lastCount int) {
	if w == nil {
		return false, 0, "", 0
	}
	ls := ""
	if !w.lastScan.IsZero() {
		ls = w.lastScan.Format("02.01.2006 15:04:05")
	}
	return true, int(w.interval.Minutes()), ls, w.lastCount
}

// Start begins the IMAP polling loop.
func (w *Watcher) Start(bot *tb.Bot) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	// Initial scan after a short delay
	select {
	case <-time.After(15 * time.Second):
		w.scanOnce(bot)
	case <-w.stopCh:
		return
	}

	for {
		select {
		case <-ticker.C:
			w.scanOnce(bot)
		case <-w.stopCh:
			return
		}
	}
}

// Stop stops the watcher.
func (w *Watcher) Stop() {
	close(w.stopCh)
}

// TriggerScan forces an immediate scan.
func (w *Watcher) TriggerScan() (processed, matched int, err error) {
	return 0, 0, nil
}

func (w *Watcher) scanOnce(bot *tb.Bot) {
	w.lastScan = time.Now()

	c, err := imapClient.DialTLS(fmt.Sprintf("%s:%d", w.imapHost, w.imapPort), nil)
	if err != nil {
		log.Printf("[IMAP] connection error: %v", err)
		return
	}
	defer c.Logout()

	if err := c.Login(w.user, w.password); err != nil {
		log.Printf("[IMAP] login error: %v", err)
		return
	}

	// Select INBOX
	mbox, err := c.Select("INBOX", false)
	if err != nil {
		log.Printf("[IMAP] select inbox error: %v", err)
		return
	}

	if mbox.Messages == 0 {
		log.Printf("[IMAP] inbox empty, skipping")
		w.lastCount = 0
		return
	}

	// Search for UNSEEN messages
	criteria := imap.NewSearchCriteria()
	criteria.WithoutFlags = []string{"$InfoBotProcessed"}
	uids, err := c.UidSearch(criteria)
	if err != nil {
		log.Printf("[IMAP] search error: %v", err)
		return
	}

	if len(uids) == 0 {
		w.lastCount = 0
		return
	}

	log.Printf("[IMAP] found %d unseen messages", len(uids))

	// Fetch the messages
	seqset := new(imap.SeqSet)
	seqset.AddNum(uids...)

	section := &imap.BodySectionName{}
	messages := make(chan *imap.Message, len(uids))
	done := make(chan error, 1)
	go func() {
		done <- c.UidFetch(seqset, []imap.FetchItem{section.FetchItem(), imap.FetchEnvelope}, messages)
	}()

	processed := 0
	matched := 0

	for msg := range messages {
		if msg == nil {
			continue
		}

		envelope := msg.Envelope
		if envelope == nil {
			continue
		}

		msgMessageID := envelope.MessageId
		inReplyTo := envelope.InReplyTo

		fromAddr := ""
		if len(envelope.From) > 0 {
			fromAddr = envelope.From[0].Address()
		}

		subject := envelope.Subject

		// Check if this is a bounce message
		if IsBounce(fromAddr, subject) {
			log.Printf("[IMAP] bounce detected from %s, subject: %s", fromAddr, subject)
			w.handleBounce(bot, msgMessageID, fromAddr)
			w.markSeen(c, msg)
			processed++
			continue
		}

		// Try to match with a sent request
		matchedEntry := w.matchReply(inReplyTo, msgMessageID, fromAddr, subject)

		if matchedEntry == nil {
			log.Printf("[IMAP] no match for message from %s, subject: %s", fromAddr, subject)
			w.markSeen(c, msg)
			processed++
			continue
		}

		log.Printf("[IMAP] MATCHED reply for user %d, request: %s", matchedEntry.UserID, matchedEntry.Subject)

		// Read the full message body and attachments
		bodyText, attachments, err := w.readMessage(msg, section)
		if err != nil {
			log.Printf("[IMAP] read message error: %v", err)
			bodyText = "[Не вдалося прочитати текст відповіді]"
			attachments = nil
		}

		// Deliver reply to user via Telegram
		w.deliverReply(bot, matchedEntry, fromAddr, subject, bodyText, attachments)

		// Update sent log
		if w.sentLog != nil {
			_ = w.sentLog.MarkReplied(matchedEntry.MessageID)
		}

		// Increment reply stats
		if w.stats != nil {
			w.stats.IncrementReplies()
		}

		w.markSeen(c, msg)
		matched++
		processed++
	}

	if err := <-done; err != nil {
		log.Printf("[IMAP] fetch error: %v", err)
	}

	w.lastCount = processed
	log.Printf("[IMAP] scan complete: %d processed, %d matched", processed, matched)
}

// matchReply tries to find a sent request that this email is a reply to.
func (w *Watcher) matchReply(inReplyTo, messageID, fromAddr, subject string) *sentlog.SentEntry {
	if w.sentLog == nil {
		return nil
	}

	// Method 1: Match by In-Reply-To header (most reliable)
	if inReplyTo != "" {
		entry := w.sentLog.FindByMessageID(inReplyTo)
		if entry != nil {
			return entry
		}
	}

	// Method 2: Match by subject (Re: / Fwd: prefix)
	originalSubject := subject
	for _, prefix := range []string{"Re: ", "RE: ", "Fwd: ", "FW: ", "Fw: "} {
		originalSubject = strings.TrimPrefix(originalSubject, prefix)
	}
	originalSubject = strings.TrimSpace(originalSubject)

	if originalSubject != subject {
		all := w.sentLog.ListAll()
		for i := len(all) - 1; i >= 0; i-- {
			if strings.Contains(all[i].Subject, originalSubject) {
				return &all[i]
			}
		}
	}

	return nil
}

// readMessage extracts text body and attachments from an IMAP message.
func (w *Watcher) readMessage(msg *imap.Message, section *imap.BodySectionName) (string, []string, error) {
	r := msg.GetBody(section)
	if r == nil {
		return "", nil, fmt.Errorf("no message body")
	}

	mr, err := mail.CreateReader(r)
	if err != nil {
		return "", nil, fmt.Errorf("parse message: %w", err)
	}

	var bodyText string
	var attachments []string

	os.MkdirAll(w.attachDir, 0755)

	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}

		switch h := p.Header.(type) {
		case *mail.InlineHeader:
			b, err := io.ReadAll(p.Body)
			if err == nil {
				contentType := h.Get("Content-Type")
				if strings.Contains(contentType, "text/plain") || bodyText == "" {
					bodyText = string(b)
				}
			}
		case *mail.AttachmentHeader:
			filename, _ := h.Filename()
			if filename == "" {
				filename = fmt.Sprintf("attachment_%d", time.Now().UnixNano())
			}
			filename = sanitizeFilename(filename)
			filePath := filepath.Join(w.attachDir, filename)

			f, err := os.Create(filePath)
			if err != nil {
				log.Printf("[IMAP] cannot create attachment file: %v", err)
				continue
			}

			written, err := io.Copy(f, p.Body)
			f.Close()
			if err != nil {
				log.Printf("[IMAP] write attachment error: %v", err)
				os.Remove(filePath)
				continue
			}

			log.Printf("[IMAP] saved attachment: %s (%d bytes)", filename, written)
			attachments = append(attachments, filePath)
		}
	}

	if bodyText == "" {
		bodyText = "[Текст відсутній — можливо, відповідь у вкладенні]"
	}

	if len(bodyText) > 4000 {
		bodyText = bodyText[:3950] + "\n\n[...обрізано...]"
	}

	return bodyText, attachments, nil
}

// deliverReply sends the reply to the user's Telegram chat.
func (w *Watcher) deliverReply(bot *tb.Bot, entry *sentlog.SentEntry, fromAddr, subject, bodyText string, attachments []string) {
	chatID := entry.ChatID
	if chatID == 0 {
		chatID = entry.UserID
	}

	text := fmt.Sprintf("📨 *Отримана відповідь на ваш запит!*\n\n"+
		"🏛 Від: %s\n"+
		"📂 Тема: %s\n"+
		"📅 Дата: %s\n\n"+
		"📝 Текст відповіді:\n%s",
		fromAddr, subject,
		time.Now().Format("02.01.2006 15:04"),
		bodyText)

	_, err := bot.Send(tb.ChatID(chatID), text, tb.ModeMarkdown)
	if err != nil {
		log.Printf("[IMAP] failed to send reply to chat %d: %v", chatID, err)
		return
	}

	for _, filePath := range attachments {
		w.sendAttachment(bot, chatID, filePath)
	}
}

// sendAttachment sends a file attachment to the user's chat.
func (w *Watcher) sendAttachment(bot *tb.Bot, chatID int64, filePath string) {
	defer func() {
		if err := os.Remove(filePath); err != nil {
			log.Printf("[IMAP] failed to remove attachment %s: %v", filePath, err)
		}
	}()
	ext := strings.ToLower(filepath.Ext(filePath))

	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".bmp", ".webp":
		photo := &tb.Photo{File: tb.FromDisk(filePath)}
		_, err := bot.Send(tb.ChatID(chatID), photo)
		if err != nil {
			log.Printf("[IMAP] failed to send image to chat %d: %v", chatID, err)
		}
	default:
		// PDF and all other files as document
		doc := &tb.Document{
			File:     tb.FromDisk(filePath),
			FileName: filepath.Base(filePath),
		}
		_, err := bot.Send(tb.ChatID(chatID), doc)
		if err != nil {
			log.Printf("[IMAP] failed to send document to chat %d: %v", chatID, err)
		}
	}
}

// handleBounce processes a bounced email notification.
func (w *Watcher) handleBounce(bot *tb.Bot, messageID, fromAddr string) {
	if w.sentLog == nil {
		return
	}

	entry := w.sentLog.FindByMessageID(messageID)
	if entry != nil {
		_ = w.sentLog.MarkBounced(entry.RecipientEmail)

		// Increment bounce stats
		if w.stats != nil {
			w.stats.IncrementBounced()
		}

		text := fmt.Sprintf("⚠️ *Лист не доставлено!*\n\n"+
			"Ваш запит до **%s** (%s) не був доставлений.\n"+
			"Можливо, адреса електронної пошти органу є недійсною.\n\n"+
			"Спробуйте знайти актуальну адресу: /directory",
			entry.RecipientName, entry.RecipientEmail)

		_, err := bot.Send(tb.ChatID(entry.ChatID), text, tb.ModeMarkdown)
		if err != nil {
			log.Printf("[IMAP] failed to notify bounce to chat %d: %v", entry.ChatID, err)
		}
	}
}

// markSeen marks a message as seen (read) in the mailbox.
func (w *Watcher) markSeen(c *imapClient.Client, msg *imap.Message) {
	if msg == nil || msg.Uid == 0 {
		return
	}
	seqset := new(imap.SeqSet)
	seqset.AddNum(msg.Uid)
	item := imap.FormatFlagsOp(imap.AddFlags, true)
	flags := []interface{}{imap.SeenFlag, "$InfoBotProcessed"}
	_ = c.UidStore(seqset, item, flags, nil)
}

// sanitizeFilename removes potentially dangerous characters from filenames.
func sanitizeFilename(name string) string {
	name = filepath.Base(name)
	// Keep only safe characters
	var safe strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '-' || r == '_' || r == ' ' {
			safe.WriteRune(r)
		} else {
			safe.WriteRune('_')
		}
	}
	name = strings.TrimSpace(name)
	if name == "" || name == "." {
		name = "attachment"
	}
	if len(name) > 200 {
		ext := filepath.Ext(name)
		name = name[:200-len(ext)] + ext
	}
	return name
}

// IsBounce checks if an email is a delivery failure notification.
func IsBounce(from, subject string) bool {
	fromLower := strings.ToLower(from)
	subjLower := strings.ToLower(subject)
	return strings.Contains(fromLower, "mailer-daemon") ||
		strings.Contains(fromLower, "postmaster") ||
		strings.Contains(subjLower, "delivery status") ||
		strings.Contains(subjLower, "undelivered") ||
		strings.Contains(subjLower, "returned mail") ||
		strings.Contains(subjLower, "mail delivery failed") ||
		strings.Contains(subjLower, "недоставлено")
}

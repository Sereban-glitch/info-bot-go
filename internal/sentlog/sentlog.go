package sentlog

import (
        "bufio"
        "encoding/json"
        "os"
        "path/filepath"
        "sync"
        "time"
)

type SentEntry struct {
        MessageID       string `json:"messageId"`
        ChatID          int64  `json:"chatId"`
        UserID          int64  `json:"userId"`
        RecipientName   string `json:"recipientName"`
        RecipientEmail  string `json:"recipientEmail"`
        Subject         string `json:"subject"`
        Date            string `json:"date"`
        Delivered       bool   `json:"delivered,omitempty"`
        Status          string `json:"status,omitempty"`
        ReplyReceivedAt string `json:"replyReceivedAt,omitempty"`

        // Канал «Доступ до правди» (dostup.org.ua)
        Channel         string `json:"channel,omitempty"`         // "dostup" | "email" (пусто = email, старые записи)
        URL             string `json:"url,omitempty"`             // публичная страница запроса
        DostupBody      string `json:"dostupBody,omitempty"`      // орган на портале
        LastStatus      string `json:"lastStatus,omitempty"`      // последний статус портала
        StatusCheckedAt string `json:"statusCheckedAt,omitempty"` // когда проверяли в последний раз
        ResponseExcerpt string `json:"responseExcerpt,omitempty"` // краткий текст ответа органа
        LastIncomingID  string `json:"lastIncomingId,omitempty"`  // id последнего обработанного входящего портала (incoming-N)
        AckAt           string `json:"ackAt,omitempty"`           // зафиксировано авто-подтверждение (промежуточная ответ)
}

type SentLog struct {
        path  string
        mu    sync.RWMutex
        cache []SentEntry
}

func New(dir string) (*SentLog, error) {
        if err := os.MkdirAll(dir, 0755); err != nil {
                return nil, err
        }
        path := filepath.Join(dir, "_sent_log.jsonl")
        sl := &SentLog{path: path}
        if err := sl.load(); err != nil {
                sl.cache = []SentEntry{}
        }
        return sl, nil
}

func (sl *SentLog) load() error {
        f, err := os.Open(sl.path)
        if err != nil {
                if os.IsNotExist(err) {
                        sl.cache = []SentEntry{}
                        return nil
                }
                return err
        }
        defer f.Close()

        sl.cache = []SentEntry{}
        scanner := bufio.NewScanner(f)
        for scanner.Scan() {
                line := scanner.Bytes()
                if len(line) == 0 {
                        continue
                }
                var entry SentEntry
                if err := json.Unmarshal(line, &entry); err != nil {
                        continue
                }
                sl.cache = append(sl.cache, entry)
        }
        return scanner.Err()
}

func (sl *SentLog) Append(entry SentEntry) error {
        sl.mu.Lock()
        defer sl.mu.Unlock()

        sl.cache = append(sl.cache, entry)

        f, err := os.OpenFile(sl.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
        if err != nil {
                return err
        }
        defer f.Close()

        enc := json.NewEncoder(f)
        enc.SetEscapeHTML(false)
        return enc.Encode(entry)
}

func (sl *SentLog) FindByMessageID(messageID string) *SentEntry {
        sl.mu.RLock()
        defer sl.mu.RUnlock()

        normalized := normalizeMessageID(messageID)
        for i := range sl.cache {
                if normalizeMessageID(sl.cache[i].MessageID) == normalized {
                        return &sl.cache[i]
                }
        }
        return nil
}

func (sl *SentLog) MarkDelivered(messageID string) error {
        sl.mu.Lock()
        defer sl.mu.Unlock()

        normalized := normalizeMessageID(messageID)
        for i := range sl.cache {
                if normalizeMessageID(sl.cache[i].MessageID) == normalized {
                        sl.cache[i].Delivered = true
                        return sl.flush()
                }
        }
        return nil
}

func (sl *SentLog) MarkReplied(messageID string) error {
        sl.mu.Lock()
        defer sl.mu.Unlock()

        normalized := normalizeMessageID(messageID)
        for i := range sl.cache {
                if normalizeMessageID(sl.cache[i].MessageID) == normalized {
                        sl.cache[i].Status = "replied"
                        sl.cache[i].ReplyReceivedAt = time.Now().Format(time.RFC3339)
                        return sl.flush()
                }
        }
        return nil
}

func (sl *SentLog) MarkExpired(messageID string) error {
        sl.mu.Lock()
        defer sl.mu.Unlock()

        normalized := normalizeMessageID(messageID)
        for i := range sl.cache {
                if normalizeMessageID(sl.cache[i].MessageID) == normalized {
                        sl.cache[i].Status = "expired"
                        return sl.flush()
                }
        }
        return nil
}

func (sl *SentLog) MarkBounced(recipientEmail string) bool {
        sl.mu.Lock()
        defer sl.mu.Unlock()

        for i := len(sl.cache) - 1; i >= 0; i-- {
                if sl.cache[i].RecipientEmail == recipientEmail && sl.cache[i].ReplyReceivedAt == "" {
                        sl.cache[i].Status = "bounced"
                        sl.cache[i].ReplyReceivedAt = nowISO()
                        _ = sl.flush()
                        return true
                }
        }
        return false
}

func (sl *SentLog) ListByUser(userID int64) []SentEntry {
        sl.mu.RLock()
        defer sl.mu.RUnlock()

        result := []SentEntry{}
        for _, e := range sl.cache {
                if e.UserID == userID {
                        result = append(result, e)
                }
        }
        return result
}

func (sl *SentLog) ListAll() []SentEntry {
        sl.mu.RLock()
        defer sl.mu.RUnlock()

        result := make([]SentEntry, len(sl.cache))
        copy(result, sl.cache)
        return result
}

// ListPendingDostup возвращает запросы канала dostup, ещё не получившие ответ.
func (sl *SentLog) ListPendingDostup() []SentEntry {
        sl.mu.RLock()
        defer sl.mu.RUnlock()

        result := []SentEntry{}
        for _, e := range sl.cache {
                if e.Channel == "dostup" && e.ReplyReceivedAt == "" && e.Status != "bounced" {
                        result = append(result, e)
                }
        }
        return result
}

// UpdateDostupStatus обновляет статус портала у записи (по MessageID).
// replied=true — содержательный ответ получен (счёт засчитан);
// lastIncomingID — идентификатор входящего для дедупликации уведомлений.
func (sl *SentLog) UpdateDostupStatus(messageID, status, excerpt, lastIncomingID string, replied bool) error {
        sl.mu.Lock()
        defer sl.mu.Unlock()

        normalized := normalizeMessageID(messageID)
        for i := range sl.cache {
                if normalizeMessageID(sl.cache[i].MessageID) != normalized {
                        continue
                }
                sl.cache[i].LastStatus = status
                sl.cache[i].StatusCheckedAt = nowISO()
                if excerpt != "" {
                        sl.cache[i].ResponseExcerpt = excerpt
                }
                if lastIncomingID != "" {
                        sl.cache[i].LastIncomingID = lastIncomingID
                }
                if replied && sl.cache[i].ReplyReceivedAt == "" {
                        sl.cache[i].Status = "replied"
                        sl.cache[i].ReplyReceivedAt = nowISO()
                }
                return sl.flush()
        }
        return nil
}

// MarkAcknowledged фиксирует промежуточную (авто-)ответ органа —
// подтверждение получения запроса. Ответом по существу НЕ считается:
// запрос остаётся в ожидании, счётчик ответов не трогаем.
func (sl *SentLog) MarkAcknowledged(messageID, status, excerpt, lastIncomingID string) error {
        sl.mu.Lock()
        defer sl.mu.Unlock()

        normalized := normalizeMessageID(messageID)
        for i := range sl.cache {
                if normalizeMessageID(sl.cache[i].MessageID) != normalized {
                        continue
                }
                sl.cache[i].LastStatus = status
                sl.cache[i].StatusCheckedAt = nowISO()
                if excerpt != "" {
                        sl.cache[i].ResponseExcerpt = excerpt
                }
                if lastIncomingID != "" {
                        sl.cache[i].LastIncomingID = lastIncomingID
                }
                if sl.cache[i].AckAt == "" {
                        sl.cache[i].AckAt = nowISO()
                }
                return sl.flush()
        }
        return nil
}

// UnmarkReplied откатывает ошибочно засчитанный ответ: орган прислал
// только авто-подтверждение, а запись уже помечена replied.
// Возвращает запись в ожидание ответа по существу.
func (sl *SentLog) UnmarkReplied(messageID, status, excerpt, lastIncomingID string) error {
        sl.mu.Lock()
        defer sl.mu.Unlock()

        normalized := normalizeMessageID(messageID)
        for i := range sl.cache {
                if normalizeMessageID(sl.cache[i].MessageID) != normalized {
                        continue
                }
                sl.cache[i].Status = ""
                sl.cache[i].ReplyReceivedAt = ""
                sl.cache[i].LastStatus = status
                sl.cache[i].StatusCheckedAt = nowISO()
                if excerpt != "" {
                        sl.cache[i].ResponseExcerpt = excerpt
                }
                if lastIncomingID != "" {
                        sl.cache[i].LastIncomingID = lastIncomingID
                }
                sl.cache[i].AckAt = nowISO()
                return sl.flush()
        }
        return nil
}

func (sl *SentLog) Close() error {
        sl.mu.Lock()
        defer sl.mu.Unlock()
        return sl.flush()
}

func (sl *SentLog) flush() error {
        // Атомарная запись: сначала во временный файл, затем rename.
        // Прежний вариант (os.Create прямо по пути) усекал файл ДО записи —
        // падение процесса в этот момент теряло весь журнал.
        tmp := sl.path + ".tmp"
        f, err := os.Create(tmp)
        if err != nil {
                return err
        }
        enc := json.NewEncoder(f)
        enc.SetEscapeHTML(false)
        for _, entry := range sl.cache {
                if err := enc.Encode(entry); err != nil {
                        f.Close()
                        os.Remove(tmp)
                        return err
                }
        }
        if err := f.Close(); err != nil {
                os.Remove(tmp)
                return err
        }
        if err := os.Rename(tmp, sl.path); err != nil {
                os.Remove(tmp)
                return err
        }
        return nil
}

func normalizeMessageID(id string) string {
        result := id
        if len(result) > 0 && result[0] == '<' {
                result = result[1:]
        }
        if len(result) > 0 && result[len(result)-1] == '>' {
                result = result[:len(result)-1]
        }
        return result
}

func nowISO() string {
        return time.Now().Format(time.RFC3339)
}

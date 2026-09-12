package web

import (
	"fmt"
	"net/http"
	"sync"
	"time"
)

// EventHub — шина подій для SSE-стрічки «Мої запити». Підписник = буфери-
// зований канал по userID. Notify не блокує відправника: переповнений
// канал просто пропускає подію — клієнт все одно перечитає список
// заново за подією або при перепідключенні.
type EventHub struct {
	mu   sync.RWMutex
	subs map[int64]map[chan string]struct{}
}

// NewEventHub creates a new event hub.
func NewEventHub() *EventHub {
	return &EventHub{subs: make(map[int64]map[chan string]struct{})}
}

func (h *EventHub) subscribe(userID int64) (<-chan string, func()) {
	ch := make(chan string, 8)
	h.mu.Lock()
	if h.subs[userID] == nil {
		h.subs[userID] = make(map[chan string]struct{})
	}
	h.subs[userID][ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		if m := h.subs[userID]; m != nil {
			delete(m, ch)
			if len(m) == 0 {
				delete(h.subs, userID)
			}
		}
		h.mu.Unlock()
	}
}

// Notify розсилає подію всім активним підписникам userID.
func (h *EventHub) Notify(userID int64, payload string) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.subs[userID] {
		select {
		case ch <- payload:
		default:
		}
	}
}

// userIDs повертає знімок активних підписників (для фонового монітора).
func (h *EventHub) userIDs() []int64 {
	h.mu.RLock()
	defer h.mu.RUnlock()
	ids := make([]int64, 0, len(h.subs))
	for id := range h.subs {
		ids = append(ids, id)
	}
	return ids
}

// handleEvents — SSE-стрім /api/events. authMiddleware вже перевірив init_data
// і поклав userID в заголовок X-User-ID. Клієнт (EventSource) підключається,
// отримує heartbeat раз на 20 секунд і перепідключається сам при обриві.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, APIResponse{OK: false, Err: "method not allowed"})
		return
	}
	if s.events == nil {
		writeJSON(w, http.StatusServiceUnavailable, APIResponse{OK: false, Err: "events disabled"})
		return
	}
	userID := getUserID(r)
	if userID == 0 {
		writeJSON(w, http.StatusUnauthorized, APIResponse{OK: false, Err: "unauthorized"})
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, APIResponse{OK: false, Err: "streaming unsupported"})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ch, unsub := s.events.subscribe(userID)
	defer unsub()

	fmt.Fprint(w, "retry: 3000\n\n")
	flusher.Flush()
	fmt.Fprint(w, "event: hello\ndata: {}\n\n")
	flusher.Flush()

	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case payload := <-ch:
			fmt.Fprintf(w, "event: update\ndata: %s\n\n", payload)
			flusher.Flush()
		case <-heartbeat.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

// startEventMonitor — фонова горутина: раз на 30 секунд порівнює кількість
// записів журналу кожного активного підписника. Журнал змінюється, коли бот
// подає запит, DostupSync ловить відповідь органа або змінюється статус —
// тоді шлемо подію, і застосунок мовчки оновлює список «Мої запити».
func (s *Server) startEventMonitor() {
	if s.events == nil || s.sentLog == nil {
		return
	}
	go func() {
		counts := make(map[int64][2]int)
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			for _, uid := range s.events.userIDs() {
				entries := s.sentLog.ListByUser(uid)
				total, replied := len(entries), 0
				for _, e := range entries {
					if e.ReplyReceivedAt != "" {
						replied++
					}
				}
				prev, seen := counts[uid]
				if seen && (prev[0] != total || prev[1] != replied) {
					s.events.Notify(uid, `{"type":"statuses"}`)
				}
				counts[uid] = [2]int{total, replied}
			}
		}
	}()
}

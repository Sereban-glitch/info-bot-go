package handlers

// Реестр коротких ссылок для callback-данных Telegram.
//
// Telegram ограничивает callback_data 64 байтами, а слаги органов
// портала бывают по 90+ символов
// (например tieritorialnie_upravlinnia_dierzhavnogho_biuro_rozsliduvan_...).
// Кнопки выбора органа с таким слагом отваливаются с BUTTON_DATA_INVALID —
// это и было причиной «мини-приложение отвалилось»-подобных мёртвых кнопок
// в каталоге (4 ошибки в логе прода 28.08 16:19).
//
// Решение: кнопка несёт короткую ссылку "r:<N>", а слаг лежит в реестре.

import (
	"fmt"
	"strconv"
	"sync"
)

type pickRegistry struct {
	mu   sync.Mutex
	seq  map[int64]int
	data map[int64]map[string]string // userID -> ref -> slug
}

var picks = &pickRegistry{
	seq:  map[int64]int{},
	data: map[int64]map[string]string{},
}

// registerPick запоминает слаг и возвращает короткую ссылку вида "r:42".
func registerPick(userID int64, slug string) string {
	picks.mu.Lock()
	defer picks.mu.Unlock()

	// Переиспользуем существующую ссылку, если слаг уже зарегистрирован
	m := picks.data[userID]
	if m == nil {
		m = map[string]string{}
		picks.data[userID] = m
	}
	for ref, s := range m {
		if s == slug {
			return ref
		}
	}

	picks.seq[userID]++
	ref := fmt.Sprintf("r:%d", picks.seq[userID])
	m[ref] = slug

	// Реестр не растёт бесконечно: держим последние 64 записи
	if len(m) > 64 {
		for ref2 := range m {
			delete(m, ref2)
			if len(m) <= 64 {
				break
			}
		}
	}
	return ref
}

// lookupPick возвращает слаг по короткой ссылке (пустая строка — не найдено).
func lookupPick(userID int64, ref string) string {
	picks.mu.Lock()
	defer picks.mu.Unlock()
	return picks.data[userID][ref]
}

// resolvePickData разворачивает данные кнопки: "r:N" → слаг, иначе — как есть.
func resolvePickData(userID int64, data string) string {
	if len(data) > 2 && data[0] == 'r' && data[1] == ':' {
		if slug := lookupPick(userID, data); slug != "" {
			return slug
		}
		return ""
	}
	return data
}

// pickRefNum — номер ссылки (для отладки).
func pickRefNum(ref string) int {
	if len(ref) > 2 && ref[0] == 'r' && ref[1] == ':' {
		n, _ := strconv.Atoi(ref[2:])
		return n
	}
	return 0
}

// forgetPicks очищает реестр коротких ссылок пользователя (ТЗ №5,
// /delete_my_data) — он в памяти, но лучше не оставлять следов.
func forgetPicks(userID int64) {
	picks.mu.Lock()
	defer picks.mu.Unlock()
	delete(picks.seq, userID)
	delete(picks.data, userID)
}

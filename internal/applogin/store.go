// Package applogin — одноразові коди входу в нативний застосунок
// («Смарт Запит»). Бот генерує код за командою /login, веб-сервер
// обмінює його на підписаний initData (той самий HMAC, що й Telegram).
package applogin

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"sync"
	"time"
)

const (
	codeTTL   = 30 * time.Minute
	maxStored = 100 // одночасних невикористаних кодів
)

type entry struct {
	userID  int64
	expires time.Time
}

// Store тримає коди в пам'яті. Термін життя коду — 30 хвилин,
// використаний код видаляється (одноразовість).
type Store struct {
	mu    sync.Mutex
	codes map[string]entry
}

func New() *Store {
	return &Store{codes: make(map[string]entry)}
}

// Generate створює 6-значний код для користувача. Прострочені та
// зайві записи підмітаються, щоб мапа не росла безмежно.
func (s *Store) Generate(userID int64) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	code := random6()
	s.codes[code] = entry{userID: userID, expires: time.Now().Add(codeTTL)}

	now := time.Now()
	for k, e := range s.codes {
		if now.After(e.expires) {
			delete(s.codes, k)
		}
	}
	for k := range s.codes {
		if len(s.codes) <= maxStored {
			break
		}
		delete(s.codes, k)
	}
	return code
}

// Exchange одноразово обмінює код на user ID. Код дійсний, поки
// не минув TTL, і видаляється при першому використанні.
func (s *Store) Exchange(code string) (int64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok := s.codes[code]
	if !ok || time.Now().After(e.expires) {
		delete(s.codes, code)
		return 0, false
	}
	delete(s.codes, code)
	return e.userID, true
}

func random6() string {
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		// криптоджерело недоступне — не блокуємо вхід, код на основі часу
		return fmt.Sprintf("%06d", time.Now().UnixNano()%1000000)
	}
	return fmt.Sprintf("%06d", n.Int64())
}

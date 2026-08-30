package stars

// Монетизация через Telegram Stars (XTR) — КАРКАС, по умолчанию ВЫКЛЮЧЕН.
//
// Активация (когда пользователей станет достаточно): в .env добавить
//   STARS_ENABLED=true
// и перезапустить бота. Всё остальное (счётчик кредитов, кнопка покупки,
// зачисление после оплаты, списание за розбор) подхватится автоматически.
//
// Модель:
//   • 1 кредит = 1 AI-розбор ответа органа;
//   • пакет STARS_ANALYZE_PACK кредитов за STARS_ANALYZE_PRICE Stars;
//   • новому пользователю один раз даётся STARS_FREE_CREDITS бесплатных;
//   • пока STARS_ENABLED=false — все розборы бесплатны, гейт не работает,
//     кредиты не списываются (текущее поведение не меняется вообще).
//
// Хранилище — JSON-файл (балансы + выданные приветственные бонусы +
// ID проведённых платежей для защиты от двойного зачисления при
// повторной доставке апдейтов Telegram).

import (
	"encoding/json"
	"os"
	"strconv"
	"sync"
)

// Store — файловое хранилище кредитов пользователей.
type Store struct {
	mu   sync.Mutex
	path string
	data creditFile
}

type creditFile struct {
	Version  int             `json:"version"`
	Credits  map[string]int  `json:"credits"`  // user_id → баланс
	Welcomed map[string]bool `json:"welcomed"` // кому уже выдан стартовый бонус
	Charges  map[string]bool `json:"charges"`  // telegram_payment_charge_id → зачислено
}

// NewStore открывает (или создаёт) хранилище по пути path.
// Ошибки чтения не фатальны: начинаем с пустого файла, старый
// повреждённый файл будет перезаписан при первом сохранении.
func NewStore(path string) *Store {
	s := &Store{
		path: path,
		data: creditFile{
			Version:  1,
			Credits:  map[string]int{},
			Welcomed: map[string]bool{},
			Charges:  map[string]bool{},
		},
	}
	if b, err := os.ReadFile(path); err == nil {
		var f creditFile
		if err := json.Unmarshal(b, &f); err == nil && f.Credits != nil {
			if f.Welcomed == nil {
				f.Welcomed = map[string]bool{}
			}
			if f.Charges == nil {
				f.Charges = map[string]bool{}
			}
			f.Version = 1
			s.data = f
		}
	}
	return s
}

func key(id int64) string { return strconv.FormatInt(id, 10) }

// Balance — текущий баланс пользователя.
func (s *Store) Balance(id int64) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.Credits[key(id)]
}

// EnsureWelcome один раз начисляет новому пользователю стартовые кредиты.
// n <= 0 — ничего не делает. Возвращает фактическое начисление (0 или n).
func (s *Store) EnsureWelcome(id int64, n int) int {
	if n <= 0 {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(id)
	if s.data.Welcomed[k] {
		return 0
	}
	s.data.Welcomed[k] = true
	s.data.Credits[k] += n
	_ = saveLocked(s)
	return n
}

// Add начисляет кредиты (после оплаты или возврата за сбой).
func (s *Store) Add(id int64, n int) error {
	if n == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Credits[key(id)] += n
	return saveLocked(s)
}

// Spend списывает n кредитов, если их достаточно; иначе false.
func (s *Store) Spend(id int64, n int) bool {
	if n <= 0 {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(id)
	if s.data.Credits[k] < n {
		return false
	}
	s.data.Credits[k] -= n
	// нулевые балансы не храним — файл не растёт
	if s.data.Credits[k] == 0 {
		delete(s.data.Credits, k)
	}
	_ = saveLocked(s)
	return true
}

// WasCharged — был ли уже зачислен платёж с таким ID (защита от
// повторной доставки successful_payment).
func (s *Store) WasCharged(chargeID string) bool {
	if chargeID == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.Charges[chargeID]
}

// ChargeIfNew атомарно помечает платёж зачислённым: true — первый раз
// (можно начислять), false — дубликат (пропустить).
func (s *Store) ChargeIfNew(chargeID string) bool {
	if chargeID == "" {
		return true // без ID — начисляем (Telegram всегда его присылает)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data.Charges[chargeID] {
		return false
	}
	// гигиена: не даём карте расти бесконечно
	if len(s.data.Charges) > 5000 {
		s.data.Charges = map[string]bool{}
	}
	s.data.Charges[chargeID] = true
	_ = saveLocked(s)
	return true
}

// saveLocked пишет файл атомарно (tmp + rename). Вызывать под мьютексом.
func saveLocked(s *Store) error {
	b, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return err
	}
	return nil
}

// Stats — снимок для логов/админа.
func (s *Store) Stats() (users, credits int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, v := range s.data.Credits {
		users++
		credits += v
	}
	return users, credits
}

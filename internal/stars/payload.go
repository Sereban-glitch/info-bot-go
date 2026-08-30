package stars

// Полезная нагрузка (payload) платежа — связывает оплату с пользователем
// и количеством кредитов. Формат:
//   analyze:<user_id>:<credits>:<unix_time>
//
// Payload приходит обратно в двух апдейтах: pre_checkout_query (до
// списания Stars — можно отказаться) и successful_payment (после оплаты).
// Валидация на обоих шагах защищает от подделки: payload создаём только
// мы, и сверяем user_id из payload с отправителем апдейта.

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// PayloadPrefix — префикс наших полезных нагрузок.
const PayloadPrefix = "analyze"

// BuildPayload строит payload для покупки пакета кредитов.
func BuildPayload(userID int64, credits int) string {
	return fmt.Sprintf("%s:%d:%d:%d", PayloadPrefix, userID, credits, time.Now().Unix())
}

// ParsePayload разбирает и валидирует payload.
func ParsePayload(p string) (userID int64, credits int, ok bool) {
	parts := strings.Split(p, ":")
	if len(parts) != 4 || parts[0] != PayloadPrefix {
		return 0, 0, false
	}
	uid, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || uid <= 0 {
		return 0, 0, false
	}
	cr, err := strconv.Atoi(parts[2])
	if err != nil || cr <= 0 || cr > 1000 {
		return 0, 0, false
	}
	if _, err := strconv.ParseInt(parts[3], 10, 64); err != nil {
		return 0, 0, false
	}
	return uid, cr, true
}

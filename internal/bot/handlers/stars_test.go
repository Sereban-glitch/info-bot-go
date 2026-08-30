package handlers

// Тесты единого роутера платежей (stars.go). Главный кейс — регрессия:
// донаты /support обязаны проходить pre-checkout при ЛЮБОМ состоянии
// монетизации (STARS_ENABLED), потому что модуль stars перезаписывает
// обработчики OnCheckout/OnPayment модуля support (telebot: map-присваивание).

import (
	"testing"

	"info-bot-go/internal/stars"
)

func TestClassifyPayload(t *testing.T) {
	cases := map[string]payloadKind{
		"support_1":                  payloadSupport,
		"support_50":                 payloadSupport,
		"support_10000":              payloadSupport,
		"analyze:123:10:1700000000":  payloadAnalyze,
		"analyze:1:1:1":              payloadAnalyze,
		"analyze:123:10":             payloadUnknown, // мало частей
		"analyze:abc:10:1700000000":  payloadUnknown, // uid не число
		"analyze:123:0:1700000000":   payloadUnknown, // кредиты <= 0
		"analyze:123:1001:170000000": payloadUnknown, // кредиты > 1000
		"shop:123:10:1700000000":     payloadUnknown, // чужой префикс
		"":                           payloadUnknown,
		"support_":                   payloadSupport, // сумма проверяется на этапе инвойса
	}
	for in, want := range cases {
		if got := classifyPayload(in); got != want {
			t.Errorf("classifyPayload(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestPreCheckoutDecision(t *testing.T) {
	const (
		uid  = int64(745130167)
		pack = 10
	)
	validAnalyze := stars.BuildPayload(uid, pack)

	t.Run("донат работает при выключенной монетизации (регрессия)", func(t *testing.T) {
		// До фикса модуль stars отклонял донаты с «Оплата ще не ввімкнена».
		ok, errMsg := preCheckoutDecision(false, pack, "support_50", uid)
		if !ok || errMsg != "" {
			t.Fatalf("донат при StarsEnabled=false отклонён: ok=%v msg=%q", ok, errMsg)
		}
	})

	t.Run("донат работает при включённой монетизации", func(t *testing.T) {
		ok, errMsg := preCheckoutDecision(true, pack, "support_50", uid)
		if !ok || errMsg != "" {
			t.Fatalf("донат при StarsEnabled=true отклонён: ok=%v msg=%q", ok, errMsg)
		}
	})

	t.Run("покупка отклоняется при выключенной монетизации", func(t *testing.T) {
		ok, errMsg := preCheckoutDecision(false, pack, validAnalyze, uid)
		if ok || errMsg == "" {
			t.Fatalf("покупка при StarsEnabled=false должна отклоняться: ok=%v msg=%q", ok, errMsg)
		}
	})

	t.Run("валидная покупка подтверждается", func(t *testing.T) {
		ok, errMsg := preCheckoutDecision(true, pack, validAnalyze, uid)
		if !ok || errMsg != "" {
			t.Fatalf("валидная покупка отклонена: ok=%v msg=%q", ok, errMsg)
		}
	})

	t.Run("чужой payload (не свой uid) отклоняется", func(t *testing.T) {
		forged := stars.BuildPayload(uid+1, pack)
		ok, _ := preCheckoutDecision(true, pack, forged, uid)
		if ok {
			t.Fatal("payload другого пользователя прошёл pre-checkout")
		}
	})

	t.Run("несовпадение размера пакета отклоняется", func(t *testing.T) {
		otherPack := stars.BuildPayload(uid, pack+5)
		ok, _ := preCheckoutDecision(true, pack, otherPack, uid)
		if ok {
			t.Fatal("payload с чужим размером пакета прошёл pre-checkout")
		}
	})

	t.Run("неизвестный payload отклоняется", func(t *testing.T) {
		ok, errMsg := preCheckoutDecision(true, pack, "shop:123:10:1", uid)
		if ok || errMsg == "" {
			t.Fatalf("неизвестный payload принят: ok=%v msg=%q", ok, errMsg)
		}
	})
}

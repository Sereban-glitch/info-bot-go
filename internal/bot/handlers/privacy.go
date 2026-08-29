package handlers

import (
	"fmt"

	tb "gopkg.in/telebot.v3"

	"info-bot-go/internal/session"
)

// PrivacyModule — ТЗ №5 «Приватність і цілісність даних».
//
// /delete_my_data — право пользователя на удаление персональных данных
// (ЗУ №2297-IV «Про захист персональних даних», ст. 8 — право на доступ,
// ст. 12 — уточнение и удаление). Двухшаговое подтверждение защищает от
// случайного нажатия: удаление необратимо.
//
// Что удаляется:
//   • профиль (ФИО, email, почтовый адрес) и черновики — файл сессии;
//   • история запросов и переписки с органами (локальный журнал);
//   • гилки уточнений (/followup);
//   • временные выборы органов (реестр кнопок).
//
// Честное ограничение: запросы, отправленные через портал «Доступ до
// правди», уже ОПУБЛИКОВАНЫ на его публичных страницах — это чужой сайт,
// мы не можем удалить их оттуда. Пользователю прямо об этом говорим.
// Глобальные счётчики (сколько всего запросов/ответов) остаются — они
// анонимны и не содержат персональных данных.

type PrivacyModule struct {
	deps *Deps
	bot  *tb.Bot
}

func NewPrivacyModule(deps *Deps) *PrivacyModule {
	return &PrivacyModule{deps: deps, bot: deps.Bot}
}

func (m *PrivacyModule) Name() string { return "privacy" }

func (m *PrivacyModule) Register() {
	m.bot.Handle("/delete_my_data", safeHandler("privacy", m.askDelete))
	m.bot.Handle("/privacy", safeHandler("privacy_info", m.showInfo))
	m.bot.Handle(&tb.Btn{Unique: "prv_del_yes"}, safeHandler("privacy_confirm", m.confirmDelete))
	m.bot.Handle(&tb.Btn{Unique: "prv_del_no"}, safeHandler("privacy_cancel", m.cancelDelete))
}

// showInfo — /privacy: что бот хранит и зачем (прозрачность, ТЗ №5).
func (m *PrivacyModule) showInfo(c tb.Context) error {
	text := "🔒 <b>Ваші дані та приватність</b>\n\n" +
		"Що бот зберігає про вас:\n" +
		"• <b>Профіль</b> — ім'я, прізвище, електронна пошта, поштова адреса. Потрібні, щоб підписувати запити (ст. 19 ЗУ «Про доступ до публічної інформації» — запит підписується іменем).\n" +
		"• <b>Чернетки</b> — незавершені запити, щоб не вводити все заново.\n" +
		"• <b>Історію запитів</b> — кому, коли, про що ви писали, і відповіді органів. Потрібна для нагадувань про терміни (5 робочих днів) і статистики.\n" +
		"• <b>Гілки уточнень</b> — переписка з органами після запиту.\n\n" +
		"Дані зберігаються на сервері бота (нічого зайвого не збираємо): файли доступні лише сервісу.\n\n" +
		"⚠️ <b>Важливо про портал «Доступ до правди»</b>: запити, надіслані через портал, публічні — їх бачать усі на dostup.org.ua. Це правило самого порталу, і ми не можемо видалити їх звідти. Через портал радимо не вказувати зайвих особистих даних.\n\n" +
		"Ви можете видалити всі свої дані з бота будь-якої миті: /delete_my_data"

	return c.Send(text, tb.ModeHTML)
}

// askDelete — шаг 1: предупреждение + кнопки подтверждения.
func (m *PrivacyModule) askDelete(c tb.Context) error {
	kb := &tb.ReplyMarkup{}
	yes := tb.Btn{Unique: "prv_del_yes", Text: "🗑 Так, видалити всі мої дані"}
	no := tb.Btn{Unique: "prv_del_no", Text: "❌ Скасувати"}
	kb.Inline(kb.Row(yes, no))

	text := "🗑 <b>Видалення ваших даних</b>\n\n" +
		"Буде видалено <b>назавжди</b>:\n" +
		"• профіль (ім'я, прізвище, пошту, адресу);\n" +
		"• чернетки;\n" +
		"• історію ваших запитів і відповідей органів;\n" +
		"• гілки уточнень.\n\n" +
		"Що <b>не можна</b> видалити:\n" +
		"• запити, вже надіслані через портал «Доступ до правди», — вони публічні на dostup.org.ua;\n" +
		"• загальну анонімну статистику бота (без ваших особистих даних).\n\n" +
		"Ви впевнені?"

	return c.Send(text, tb.ModeHTML, kb)
}

// confirmDelete — шаг 2: удаление выполнено.
func (m *PrivacyModule) confirmDelete(c tb.Context) error {
	_ = c.Respond()
	userID := c.Sender().ID
	key := session.SessionKey(userID)

	// Сессия сейчас под блокировкой middleware; удаляем черновики/профиль
	// через файловое хранилище (кэш + файл на диске).
	m.deps.Sessions.Delete(key)

	// История запросов и переписки.
	removed := 0
	if m.deps.SentLog != nil {
		n, err := m.deps.SentLog.DeleteByUser(userID)
		if err != nil {
			// ошибка удаления — сообщаем честно, частичного молчания не допускаем
			_ = c.Edit(fmt.Sprintf("⚠️ Історія запитів: помилка видалення (%v). Профіль і чернетки видалено. Напишіть @sereban_tech — розберемось.", err))
			return nil
		}
		removed = n
	}

	// Гилки уточнений.
	if m.deps.FollowUps != nil {
		m.deps.FollowUps.DeleteByUser(userID)
	}

	// Реестр выборов органов (кнопки каталога).
	forgetPicks(userID)

	text := "✅ <b>Ваші дані видалено</b>\n\n" +
		fmt.Sprintf("• Профіль і чернетки — видалено\n• Історія запитів — видалено (%d запис(ів))\n• Гілки уточнень — видалено\n\n", removed) +
		"Нагадуємо: запити, які вже публічні на dostup.org.ua, залишаються там — це сторінки порталу, ми не маємо до них адмін-доступу.\n\n" +
		"Почати з чистого аркуша: /start"
	_ = c.Edit(text, tb.ModeHTML)
	return nil
}

// cancelDelete — передумали.
func (m *PrivacyModule) cancelDelete(c tb.Context) error {
	_ = c.Respond()
	_ = c.Edit("👍 Скасовано. Ваші дані на місці.")
	return nil
}

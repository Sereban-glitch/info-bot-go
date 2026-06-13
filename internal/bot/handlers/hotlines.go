package handlers

import (
	"fmt"

	tb "gopkg.in/telebot.v3"
)

var hotlineData = map[string]struct {
	Title  string
	Phone  string
	Script string
	Tip    string
}{
	"army": {
		Title:  "🎖 ТЦК, ВВК та ЗСУ",
		Phone:  "1512",
		Script: "«Я звертаюся щодо порушення прав [військовослужбовця/військовозобов'язаного] у [вказати підрозділ або ТЦК]. Суть проблеми: ...»",
		Tip:    "Обов'язково вимагайте номер вашого звернення (вхідний). Запишіть час дзвінка та ПІБ оператора.",
	},
	"rights": {
		Title:  "⚖️ Захист прав (Омбудсмен)",
		Phone:  "0 800 50 17 20",
		Script: "«Мої конституційні права були порушені діями [назва органу]. Прошу зафіксувати заяву про порушення Закону про доступ до інформації...»",
		Tip:    "Ця лінія найкраще працює, коли державний орган ігнорує ваші запити більше 30 днів.",
	},
	"vpo": {
		Title:  "🤝 Допомога ВПО та виплати",
		Phone:  "1548",
		Script: "«Я є ВПО, зареєстрований за адресою [...]. Моє питання стосується [неотримання виплат / проблем із житлом]...»",
		Tip:    "Для розмови тримайте під рукою довідку ВПО та свій РНОКПП (ідентифікаційний код).",
	},
	"police": {
		Title:  "🚔 Поліція (Лінія довіри)",
		Phone:  "0 800 50 02 02",
		Script: "«Повідомляю про неправомірні дії / бездіяльність співробітників поліції [номер відділку]. Суть інциденту: ...»",
		Tip:    "Якщо ви стали свідком злочину, що триває прямо зараз — телефонуйте лише на 102.",
	},
	"energy": {
		Title:  "💡 Енергетика та ЖКГ (Уряд)",
		Phone:  "1545",
		Script: "«Моє звернення стосується несправедливого розподілу електроенергії / відсутності води за адресою [...]...»",
		Tip:    "Це загальна урядова лінія. Вона фіксує все — від ям на дорогах до корупції в мерії.",
	},
}

// HotlinesModule handles /hotlines.
type HotlinesModule struct {
	deps *Deps
	bot  *tb.Bot
}

func NewHotlinesModule(deps *Deps) *HotlinesModule {
	return &HotlinesModule{deps: deps, bot: deps.Bot}
}

func (m *HotlinesModule) Name() string { return "hotlines" }

func (m *HotlinesModule) Register() {
	handler := safeHandler("hotlines", func(c tb.Context) error {
		kb := &tb.ReplyMarkup{}
		var rows [][]tb.InlineButton
		for key, data := range hotlineData {
			rows = append(rows, []tb.InlineButton{
				{Unique: "hl", Text: data.Title, Data: key},
			})
		}
		kb.InlineKeyboard = rows

		return c.Send("📞 *Довідник гарячих ліній та алгоритмів дії*\n\nОберіть категорію проблеми:", kb, tb.ModeMarkdown)
	})

	m.bot.Handle("/hotlines", handler)
	m.bot.Handle("📞 Гарячі лінії", handler)

	// Hotline category callback
	hlBtn := tb.InlineButton{Unique: "hl"}
	m.bot.Handle(&hlBtn, safeHandler("hl_detail", func(c tb.Context) error {
		_ = c.Respond()
		key := c.Callback().Data
		data, ok := hotlineData[key]
		if !ok {
			return nil
		}

		text := fmt.Sprintf("📞 *%s*\n\n☎️ Номер: %s\n\n🗣 *Що сказати (Скрипт):*\n_%s_\n\n💡 *Важлива порада:*\n%s",
			data.Title, data.Phone, data.Script, data.Tip)

		kb := &tb.ReplyMarkup{}
		kb.InlineKeyboard = [][]tb.InlineButton{
			{{Unique: "hl_back", Text: "⬅️ Назад до списку"}},
		}

		_ = c.Edit(text, kb, tb.ModeMarkdown)
		return nil
	}))

	// Back to list
	hlBackBtn := tb.InlineButton{Unique: "hl_back"}
	m.bot.Handle(&hlBackBtn, handler)
}

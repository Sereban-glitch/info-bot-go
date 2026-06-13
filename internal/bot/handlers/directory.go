package handlers

import (
	"fmt"
	"strings"

	tb "gopkg.in/telebot.v3"
)

// DirectoryModule handles /directory and /find.
type DirectoryModule struct {
	deps *Deps
	bot  *tb.Bot
}

func NewDirectoryModule(deps *Deps) *DirectoryModule {
	return &DirectoryModule{deps: deps, bot: deps.Bot}
}

func (m *DirectoryModule) Name() string { return "directory" }

func (m *DirectoryModule) Register() {
	m.bot.Handle("/directory", safeHandler("directory", func(c tb.Context) error {
		labels := m.deps.Directory.CategoryLabels()
		keys := m.deps.Directory.CategoryKeys()
		all := m.deps.Directory.AllRecipients()

		text := fmt.Sprintf("📚 Довідник розпорядників (усього %d):\n\n", len(all))
		for _, k := range keys {
			label := labels[k]
			items := m.deps.Directory.ByCategory(k)
			text += fmt.Sprintf("%s (%d):\n", label, len(items))
			for _, r := range items {
				text += fmt.Sprintf("• %s — %s\n", r.Name, r.Email)
			}
			text += "\n"
		}

		if len(text) > 4000 {
			text = text[:4000] + "..."
		}
		return c.Send(text)
	}))

	m.bot.Handle("/find", safeHandler("find", func(c tb.Context) error {
		args := c.Message().Payload
		q := strings.TrimSpace(args)
		if q == "" {
			return c.Send("Використання: /find <слово>\nНаприклад: /find поліція")
		}

		found := m.deps.Directory.Search(q)
		if len(found) == 0 {
			return c.Send(fmt.Sprintf("🔎 По запиту «%s» нічого не знайдено.", q))
		}

		text := fmt.Sprintf("🔎 Знайдено (%d):\n\n", len(found))
		for _, r := range found {
			text += fmt.Sprintf("• %s\n  📧 %s\n\n", r.Name, r.Email)
		}
		if len(text) > 4000 {
			text = text[:4000] + "..."
		}
		return c.Send(text)
	}))
}

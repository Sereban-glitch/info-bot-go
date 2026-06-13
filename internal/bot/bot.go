package bot

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"info-bot-go/internal/ai"
	"info-bot-go/internal/bot/handlers"
	"info-bot-go/internal/config"
	"info-bot-go/internal/directory"
	"info-bot-go/internal/email"
	"info-bot-go/internal/imap"
	"info-bot-go/internal/osint"
	"info-bot-go/internal/ratelimiter"
	"info-bot-go/internal/sentlog"
	"info-bot-go/internal/session"
	"info-bot-go/internal/stats"

	tb "gopkg.in/telebot.v3"
)

// Bot wraps the telebot instance and dependencies.
type Bot struct {
	bot      *tb.Bot
	cfg      *config.Config
	sessions *session.FileStore
	sentLog  *sentlog.SentLog
	watcher  *imap.Watcher
	gemini   *ai.Rotator
	stats    *stats.Stats
	sessDir  string
	rateLim  *ratelimiter.RateLimiter
}

// New creates a new Bot with all dependencies.
func New(cfg *config.Config, sessStore *session.FileStore, sentLog *sentlog.SentLog, globalStats *stats.Stats, watcher *imap.Watcher) (*Bot, error) {
	pref := tb.Settings{
		Token:  cfg.BotToken,
		Poller: &tb.LongPoller{Timeout: 10 * time.Second},
		OnError: func(err error, c tb.Context) {
			log.Printf("[BOT ERROR] %v", err)
		},
	}

	b, err := tb.NewBot(pref)
	if err != nil {
		return nil, fmt.Errorf("create bot: %w", err)
	}

	var rotator *ai.Rotator
	if cfg.GeminiAvailable() {
		rotator = ai.NewRotator(cfg.GeminiAPIKeys, cfg.GeminiModel, cfg.GeminiFallbackModel)
	}

	var finder *osint.Finder
	if len(cfg.GeminiAPIKeys) > 0 {
		finder = osint.NewFinder(cfg.GeminiAPIKeys)
	}

	// Rate limiter: 3 requests per hour per user
	rl := ratelimiter.New(3, 1*time.Hour)

	botInst := &Bot{
		bot:      b,
		cfg:      cfg,
		sessions: sessStore,
		sentLog:  sentLog,
		watcher:  watcher,
		gemini:   rotator,
		stats:    globalStats,
		sessDir:  cfg.SessionDir,
		rateLim:  rl,
	}

	b.Use(botInst.sessionMiddleware())

	deps := &handlers.Deps{
		Cfg:       cfg,
		Sessions:  sessStore,
		SentLog:   sentLog,
		Gemini:    rotator,
		Watcher:   watcher,
		Bot:       b,
		Directory: directory.All(),
		Email:     email.NewSender(cfg),
		Stats:     globalStats,
		RateLimit: rl,
		OSINT:     finder,
	}

	modules := handlers.AllModules(deps)
	for _, m := range modules {
		if err := safeRegister(m); err != nil {
			log.Printf("[WARN] module %q failed to register: %v", m.Name(), err)
		} else {
			log.Printf("[INFO] module %q registered", m.Name())
		}
	}

	// Find the search module for idle text routing
	var searchMod *handlers.SearchModule
	for _, m := range modules {
		if sm, ok := m.(*handlers.SearchModule); ok {
			searchMod = sm
			break
		}
	}

	// Universal text dispatcher
	b.Handle(tb.OnText, func(c tb.Context) error {
		text := strings.TrimSpace(c.Text())
		if strings.HasPrefix(text, "/") {
			return nil
		}
		return dispatchText(deps, c, searchMod)
	})

	return botInst, nil
}

func (b *Bot) Telebot() *tb.Bot     { return b.bot }
func (b *Bot) Rotator() *ai.Rotator { return b.gemini }
func (b *Bot) Start()               { b.bot.Start() }

func (b *Bot) Stop() {
	if b.rateLim != nil {
		b.rateLim.Stop()
	}
	b.bot.Stop()
}

func (b *Bot) sessionMiddleware() tb.MiddlewareFunc {
	return func(next tb.HandlerFunc) tb.HandlerFunc {
		return func(c tb.Context) error {
			if c.Sender() == nil {
				return next(c)
			}
			key := session.SessionKey(c.Sender().ID)

			isNewUser := false
			sessionPath := filepath.Join(b.sessDir, key+".json")
			if _, err := os.Stat(sessionPath); os.IsNotExist(err) {
				isNewUser = true
			}

			sess, err := b.sessions.Get(key)
			if err != nil {
				sess = session.NewSessionData()
			}

			c.Set("session", sess)
			c.Set("sessionKey", key)

			err = next(c)

			_ = b.sessions.Set(key, sess)

			if isNewUser && b.stats != nil {
				b.stats.IncrementUsers()
			}

			return err
		}
	}
}

func safeRegister(m handlers.Module) (err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[PANIC] module %q registration panicked: %v", m.Name(), r)
			err = fmt.Errorf("module %q panicked: %v", m.Name(), r)
		}
	}()
	m.Register()
	return nil
}

// dispatchText routes text input to step handlers or idle search.
func dispatchText(deps *handlers.Deps, c tb.Context, searchMod *handlers.SearchModule) error {
	sess := c.Get("session")
	if sess == nil {
		return nil
	}
	sessionData, ok := sess.(*session.SessionData)
	if !ok {
		return nil
	}
	step := sessionData.Step

	// If user is in a step-based flow, route to the appropriate handler
	if step != "idle" && step != "" {
		for _, m := range handlers.AllModules(deps) {
			if handler, ok := m.(handlers.StepHandler); ok {
				if strings.HasPrefix(step, handler.StepPrefix()) {
					handled, err := handler.HandleText(c, step, c.Text())
					if err != nil {
						log.Printf("[ERROR] step handler %q crashed: %v", m.Name(), err)
						_ = c.Send("⚠️ Виникла помилка. Спробуйте /cancel і почніть заново.")
					}
					if handled {
						return nil
					}
				}
			}
		}
		return nil
	}

	// User is in idle state — try directory search
	if searchMod != nil && len(strings.TrimSpace(c.Text())) >= 3 {
		return searchMod.HandleSearch(c, strings.TrimSpace(c.Text()))
	}

	return nil
}

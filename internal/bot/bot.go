package bot

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"info-bot-go/internal/ai"
	"info-bot-go/internal/bot/handlers"
	"info-bot-go/internal/config"
	"info-bot-go/internal/directory"
	"info-bot-go/internal/dostup"
	"info-bot-go/internal/email"
	"info-bot-go/internal/imap"
	"info-bot-go/internal/osint"
	"info-bot-go/internal/ratelimiter"
	"info-bot-go/internal/safego"
	"info-bot-go/internal/sentlog"
	"info-bot-go/internal/session"
	"info-bot-go/internal/stars"
	"info-bot-go/internal/stats"

	tb "gopkg.in/telebot.v3"
)

// Bot wraps the telebot instance and dependencies.
type Bot struct {
	bot           *tb.Bot
	cfg           *config.Config
	sessions      *session.FileStore
	sentLog       *sentlog.SentLog
	watcher       *imap.Watcher
	gemini        *ai.Rotator
	stats         *stats.Stats
	sessDir       string
	stars         *stars.Store
	rateLim       *ratelimiter.RateLimiter
	sync          *handlers.DostupSync
	dostup        *dostup.Client       // канал «Доступ до правды»
	dostupCatalog *dostup.CatalogStore // локальный каталог органов
	dostupRatings *dostup.RatingsStore // рейтинги органов портала
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
		// AI-прокси: маршрутизация запросов через роутер (например
		// antigravity-claude-proxy); при сбое — автоматический
		// возврат к прямому доступу к Gemini API.
		if cfg.AIProxyURL != "" {
			rotator.SetProxy(ai.ProxyConfig{
				URL:           cfg.AIProxyURL,
				Key:           cfg.AIProxyKey,
				Model:         cfg.AIProxyModel,
				FallbackModel: cfg.AIProxyFallbackModel,
				MediaModel:    cfg.AIProxyMediaModel,
			})
		}
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

	// Монетизация Stars (каркас): хранилище кредитов всегда создаётся,
	// но списания идут только при STARS_ENABLED=true.
	starsStore := stars.NewStore(cfg.StarsStoreFile)
	botInst.stars = starsStore

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
		Stars:     starsStore,
	}

	// Канал «Доступ до правди» (dostup.org.ua): включается переменными
	// DOSTUP_EMAIL + DOSTUP_PASSWORD в .env; без них бот работает как раньше.
	if cfg.DostupEmail != "" && cfg.DostupPassword != "" {
		dc := dostup.New(cfg.DostupSessionFile)
		dc.SetCredentials(cfg.DostupEmail, cfg.DostupPassword)
		deps.Dostup = dc
		log.Printf("[INFO] dostup.org.ua channel enabled (session file: %s)", cfg.DostupSessionFile)
		// Каталог розпорядників порталу (локальный кэш + bindings)
		catalogPath := filepath.Join(cfg.DostupSessionFileDir(), "dostup_catalog.json")
		deps.DostupCatalog = dostup.NewCatalogStore(catalogPath)
		deps.DostupCatalog.Load()
		botInst.dostup = dc
		botInst.dostupCatalog = deps.DostupCatalog
		// Персистентні рейтинги органів (індекс відкритості + лідерборд)
		ratingsStore := dostup.NewRatingsStore(filepath.Join(cfg.DostupSessionFileDir(), "dostup_ratings.json"))
		ratingsStore.Load()
		dc.SetRatingsStore(ratingsStore)
		deps.DostupRatings = ratingsStore
		botInst.dostupRatings = ratingsStore
		// Гилки запросов для уточнений (followup)
		deps.FollowUps = session.NewFollowUpThreads(filepath.Join(sessDir(), "followup_threads.json"))
		// Фоновая синхронизация бот ↔ портал
		syncWorker := handlers.NewDostupSync(deps)
		deps.DostupSync = syncWorker
		botInst.sync = syncWorker
		syncWorker.Start()
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

	// ТЗ №6: модуль розбора — пересланный в покое длинный текст
	// (вероятный ответ органа) уводим на AI-разбор вместо поиска органа.
	var analyzeMod *handlers.AnalyzeModule
	for _, m := range modules {
		if am, ok := m.(*handlers.AnalyzeModule); ok {
			analyzeMod = am
			break
		}
	}

	// Подключаем модуль уточнений к sync-воркеру (напоминания о просрочках)
	if deps.DostupSync != nil {
		for _, m := range modules {
			if fu, ok := m.(*handlers.FollowUpModule); ok {
				deps.DostupSync.SetFollowUpModule(fu)
				break
			}
		}
	}

	// Universal text dispatcher.
	// ВАЖНО: передаём ЗАРЕГИСТРИРОВАННЫЕ экземпляры модулей, а не создаём
	// новые через AllModules(deps) — иначе Register() не вызывается и
	// состояние, установленное при регистрации (например, skipBtn в
	// ProfileModule), остаётся nil → panic в askNextField (падал весь
	// процесс, когда пользователь вводил имя в флоу профиля).
	b.Handle(tb.OnText, func(c tb.Context) error {
		text := strings.TrimSpace(c.Text())
		if strings.HasPrefix(text, "/") {
			return nil
		}
		return dispatchText(deps, c, searchMod, analyzeMod, modules)
	})

	return botInst, nil
}

func (b *Bot) Telebot() *tb.Bot     { return b.bot }
func (b *Bot) Rotator() *ai.Rotator { return b.gemini }
func (b *Bot) Start()               { b.bot.Start() }

// Stars — хранилище кредитов монетизации (для веб-сервера).
func (b *Bot) Stars() *stars.Store { return b.stars }

// Dostup — клиент портала «Доступ до правды» (nil — канал не настроен).
func (b *Bot) Dostup() *dostup.Client { return b.dostup }

// DostupCatalog — локальный каталог распорядителей портала.
func (b *Bot) DostupCatalog() *dostup.CatalogStore { return b.dostupCatalog }

// DostupRatings — персистентные рейтинги органов портала.
func (b *Bot) DostupRatings() *dostup.RatingsStore { return b.dostupRatings }

// sessDir — каталог сессий (для рабочих файлов).
func sessDir() string {
	if dir := os.Getenv("SESSION_DIR"); dir != "" {
		return dir
	}
	return ".sessions_go"
}

func (b *Bot) Stop() {
	if b.sync != nil {
		b.sync.Stop()
	}
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

			// ТЗ №5 — целостность при одновременной работе: вся обработка
			// сообщений одного пользователя идёт под персональной
			// блокировкой. Сообщения из двух устройств (или работа бота и
			// мини-приложения одновременно) больше не «топчут» сессию друг
			// друга и не дают гонок на общих данных.
			b.sessions.LockSession(key)
			defer b.sessions.UnlockSession(key)

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

			// Уведомление владельцу о новом пользователе: раньше владелец
			// не видел, приходят ли люди после приглашений (приходилось
			// вручную смотреть файлы). Теперь бот сам сообщает о каждом
			// новом лице — с именем, @username и счётчиком всех юзеров.
			if isNewUser {
				chatType := ""
				if c.Chat() != nil {
					chatType = string(c.Chat().Type)
				}
				total := 0
				if b.stats != nil {
					total = b.stats.Get().TotalUsers
				}
				sender := *c.Sender()
				adminID := b.cfg.AdminID
				botSend := b.bot
				safego.Go("newuser-notify", func() {
					if !shouldNotifyNewUser(sender.ID, adminID, chatType) {
						return
					}
					if _, err := botSend.Send(tb.ChatID(adminID), newUserNotifyText(sender, total)); err != nil {
						log.Printf("[NEWUSER] не удалось уведомить владельца о пользователе %d: %v", sender.ID, err)
					} else {
						log.Printf("[NEWUSER] новый пользователь %d — владелец уведомлён", sender.ID)
					}
				})
			}

			return err
		}
	}
}

// shouldNotifyNewUser решает, нужно ли уведомлять владельца о новом
// пользователе. Не уведомляем: самого владельца (он и так всё знает)
// и сообщения в группах (там «новым лицом» может оказаться любой
// участник группы, который вообще не начинал работать с ботом).
func shouldNotifyNewUser(senderID, adminID int64, chatType string) bool {
	if adminID == 0 || senderID == 0 || senderID == adminID {
		return false
	}
	return chatType == "" || chatType == string(tb.ChatPrivate)
}

// newUserNotifyText — текст уведомления владельцу о новом пользователе.
// Чистая функция — покрыта юнит-тестами.
func newUserNotifyText(s tb.User, totalUsers int) string {
	name := strings.TrimSpace(strings.TrimSpace(s.FirstName) + " " + strings.TrimSpace(s.LastName))
	if name == "" {
		name = "без імені"
	}
	username := "немає"
	if s.Username != "" {
		username = "@" + s.Username
	}
	total := ""
	if totalUsers > 0 {
		total = fmt.Sprintf("\n👥 Всього користувачів: %d", totalUsers)
	}
	return fmt.Sprintf("🎉 У «Прозоро» новий користувач!\n\n"+
		"👤 Ім'я: %s\n"+
		"🔗 Telegram: %s\n"+
		"🆔 ID: %d%s\n\n"+
		"Людей стає більше — так тримати! 🚀",
		name, username, s.ID, total)
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
// modules — зарегистрированные экземпляры модулей (те же, что в New());
// переиспользуем их, чтобы состояние из Register() было доступно.
func dispatchText(deps *handlers.Deps, c tb.Context, searchMod *handlers.SearchModule, analyzeMod *handlers.AnalyzeModule, modules []handlers.Module) error {
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
		for _, m := range modules {
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

	// ТЗ №6 «Розбір відмови»: пересланный длинный текст в покое —
	// вероятнее всего, ответ органа. Поиск органа по такому тексту всё равно
	// бесполезен, зато AI-разбор — то, что нужно (короткие пересылки
	// по-прежнему ищут орган).
	text := strings.TrimSpace(c.Text())
	if analyzeMod != nil && isForwardedLongText(c, text) {
		return analyzeMod.OfferForward(c, text)
	}

	// User is in idle state — try directory search
	if searchMod != nil && len(strings.TrimSpace(c.Text())) >= 3 {
		return searchMod.HandleSearch(c, strings.TrimSpace(c.Text()))
	}

	return nil
}

// isForwardedLongText — пересланное сообщение с достаточно длинным текстом:
// реальные ответы органов длинные, а пересланные названия органов — короткие.
func isForwardedLongText(c tb.Context, text string) bool {
	if msg := c.Message(); msg == nil || !msg.IsForwarded() {
		return false
	}
	return utf8.RuneCountInString(text) >= 60
}

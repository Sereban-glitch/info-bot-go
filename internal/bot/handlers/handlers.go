package handlers

import (
	"io"
	"time"

	tb "gopkg.in/telebot.v3"

	"info-bot-go/internal/ai"
	"info-bot-go/internal/config"
	"info-bot-go/internal/directory"
	"info-bot-go/internal/email"
	"info-bot-go/internal/imap"
	"info-bot-go/internal/osint"
	"info-bot-go/internal/ratelimiter"
	"info-bot-go/internal/sentlog"
	"info-bot-go/internal/session"
	"info-bot-go/internal/stats"
)

// Deps holds shared dependencies for all handlers.
type Deps struct {
	Cfg       *config.Config
	Sessions  *session.FileStore
	SentLog   *sentlog.SentLog
	Gemini    *ai.Rotator
	Watcher   *imap.Watcher
	Bot       *tb.Bot
	Directory *directory.Directory
	Email     *email.Sender
	Stats     *stats.Stats
	RateLimit *ratelimiter.RateLimiter
	OSINT     *osint.Finder
}

type (
	SessionData  = session.SessionData
	Profile      = session.Profile
	Draft        = session.Draft
	PRDraft      = session.PRDraft
	HistoryEntry = session.HistoryEntry
)

type Module interface {
	Name() string
	Register()
}

type StepHandler interface {
	StepPrefix() string
	HandleText(c tb.Context, step string, text string) (bool, error)
}

func AllModules(deps *Deps) []Module {
	startMod := NewStartModule(deps)
	helpMod := NewHelpModule(deps)
	cancelMod := NewCancelModule(deps)
	profileMod := NewProfileModule(deps)
	newReqMod := NewNewRequestModule(deps)
	myReqMod := NewMyRequestsModule(deps)
	dirMod := NewDirectoryModule(deps)
	tplMod := NewTemplatesModule(deps)
	hotlinesMod := NewHotlinesModule(deps)
	inboxMod := NewInboxModule(deps)
	supportMod := NewSupportModule(deps)
	voiceMod := NewVoiceModule(deps)
	copilotMod := NewCopilotModule(deps)
	backupMod := NewBackupModule(deps)
	bugReportMod := NewBugReportModule(deps)
	deadlineMod := NewDeadlineModule(deps)
	statsMod := NewStatsModule(deps)
	searchMod := NewSearchModule(deps)

	voiceMod.SetBugReportModule(bugReportMod)

	return []Module{
		startMod,
		helpMod,
		cancelMod,
		profileMod,
		newReqMod,
		myReqMod,
		dirMod,
		tplMod,
		hotlinesMod,
		inboxMod,
		supportMod,
		voiceMod,
		copilotMod,
		backupMod,
		bugReportMod,
		deadlineMod,
		statsMod,
		searchMod,
	}
}

type chatRecipient string

func (r chatRecipient) Recipient() string {
	return string(r)
}

var (
	_ = io.Copy
	_ = time.Now
	_ = stats.GlobalStats{}
	_ = ratelimiter.New
)

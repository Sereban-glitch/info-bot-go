package config

import (
        "fmt"
        "os"
        "path/filepath"
        "strconv"
        "strings"
)

type Config struct {
        // Telegram
        BotToken string

        // SMTP (generic — works with Gmail, Resend, self-hosted, etc.)
        SMTPHost     string
        SMTPPort     int
        SMTPUser     string
        SMTPPassword string
        SMTPFromAddr string

        // Legacy aliases (for backward compat)
        GmailUser        string
        GmailAppPassword string

        // Gemini AI
        GeminiAPIKeys []string
        GeminiModel   string

        // IMAP
        IMAPHost        string
        IMAPPort        int
        IMAPPollMinutes int

        // Admin
        AdminID int64

        // Channel for copilot
        ChannelID string

        // Session
        SessionDir string

        // Mini App URL
        MiniAppURL string

        // Доступ до правди (dostup.org.ua) — канал отправки информационных запросов
        DostupEmail       string // email аккаунта на портале
        DostupPassword    string // пароль аккаунта
        DostupSessionFile string // файл cookie-сессии (пусто = без персистентности)

        // Период фоновой синхронизации с порталом (минуты)
        DostupSyncMinutes int

        // Gemini fallback model
        GeminiFallbackModel string

        // Shared mailbox fallback
        SharedMailbox string

        // AI-прокси (антispam/балансировочный роутер запросов к Gemini,
        // например antigravity-claude-proxy на той же VM).
        // Пустой AI_PROXY_URL — прямой доступ к Gemini API.
        AIProxyURL           string
        AIProxyKey           string
        AIProxyModel         string
        AIProxyFallbackModel string
        AIProxyMediaModel    string
}

func Load() (*Config, error) {
        c := &Config{
                BotToken: os.Getenv("TELEGRAM_BOT_TOKEN"),

                // SMTP configuration (generic)
                // Supports: Gmail, Resend, Brevo, self-hosted Postfix, etc.
                SMTPHost:     getEnvOrDefault("SMTP_HOST", "smtp.gmail.com"),
                SMTPPort:     getEnvInt("SMTP_PORT", 587),
                SMTPUser:     getEnvOrDefault("SMTP_USER", os.Getenv("GMAIL_USER")),
                SMTPPassword: getEnvOrDefault("SMTP_PASSWORD", os.Getenv("GMAIL_APP_PASSWORD")),
                SMTPFromAddr: getEnvOrDefault("SMTP_FROM_ADDR", os.Getenv("GMAIL_USER")),

                // IMAP configuration (generic)
                IMAPHost: getEnvOrDefault("IMAP_HOST", "imap.gmail.com"),
                IMAPPort: getEnvInt("IMAP_PORT", 993),

                // Legacy fields (backward compat)
                GmailUser:        os.Getenv("GMAIL_USER"),
                GmailAppPassword: os.Getenv("GMAIL_APP_PASSWORD"),

                GeminiModel:         getEnvOrDefault("GEMINI_MODEL", "gemini-1.5-flash"),
                IMAPPollMinutes:     getEnvInt("IMAP_POLL_MINUTES", 60),
                AdminID:             getEnvInt64("ADMIN_ID", 745130167),
                ChannelID:           getEnvOrDefault("CHANNEL_ID", "@svobodnye_ludi_zp"),
                SessionDir:          getEnvOrDefault("SESSION_DIR", ".sessions_go"),
                MiniAppURL:          getEnvOrDefault("MINI_APP_URL", "https://mini-app-deployment.vercel.app/"),
                GeminiFallbackModel: getEnvOrDefault("GEMINI_FALLBACK_MODEL", "gemini-2.5-flash-lite"),
                SharedMailbox:       getEnvOrDefault("SMTP_FROM_ADDR", getEnvOrDefault("GMAIL_USER", "publicinquiry69@gmail.com")),

                // AI-прокси
                AIProxyURL:           strings.TrimSpace(os.Getenv("AI_PROXY_URL")),
                AIProxyKey:           strings.TrimSpace(os.Getenv("AI_PROXY_KEY")),
                AIProxyModel:         getEnvOrDefault("AI_PROXY_MODEL", ""),
                AIProxyFallbackModel: getEnvOrDefault("AI_PROXY_FALLBACK_MODEL", ""),
                AIProxyMediaModel:    getEnvOrDefault("AI_PROXY_MEDIA_MODEL", ""),

                // Доступ до правди
                DostupEmail:       getEnvOrDefault("DOSTUP_EMAIL", ""),
                DostupPassword:    getEnvOrDefault("DOSTUP_PASSWORD", ""),
                DostupSessionFile: getEnvOrDefault("DOSTUP_SESSION_FILE", ".dostup_session.json"),
        }

        // If SMTP_USER is not set, fall back to GMAIL_USER
        if c.SMTPUser == "" {
                c.SMTPUser = c.GmailUser
        }
        if c.SMTPPassword == "" {
                c.SMTPPassword = c.GmailAppPassword
        }
        if c.SMTPFromAddr == "" {
                c.SMTPFromAddr = c.GmailUser
        }

        // Parse Gemini keys (comma-separated)
        rawKeys := os.Getenv("GEMINI_API_KEY")
        if rawKeys == "" {
                rawKeys = os.Getenv("GOOGLE_API_KEY")
        }
        if rawKeys != "" {
                for _, k := range strings.Split(rawKeys, ",") {
                        k = strings.TrimSpace(k)
                        if k != "" {
                                c.GeminiAPIKeys = append(c.GeminiAPIKeys, k)
                        }
                }
        }

        if c.BotToken == "" {
                return nil, fmt.Errorf("TELEGRAM_BOT_TOKEN is required")
        }

        return c, nil
}

// GeminiAvailable returns true if at least one Gemini API key is configured.
func (c *Config) GeminiAvailable() bool {
        return len(c.GeminiAPIKeys) > 0
}

// DostupSessionFileDir — каталог, где лежат рабочие файлы dostup-канала
// (каталог файла сессии; как правило — рабочая папка бота). Здесь же
// хранятся dostup_catalog.json и dostup_bindings.json — совместимо
// с файлами, созданными предыдущей версией на проде.
func (c *Config) DostupSessionFileDir() string {
        if c.DostupSessionFile == "" {
                return "."
        }
        dir := filepath.Dir(c.DostupSessionFile)
        if dir == "" {
                return "."
        }
        return dir
}

// SMTPAddr returns the full SMTP server address (host:port).
func (c *Config) SMTPAddr() string {
        return fmt.Sprintf("%s:%d", c.SMTPHost, c.SMTPPort)
}

func getEnvOrDefault(key, def string) string {
        v := os.Getenv(key)
        if v == "" {
                return def
        }
        return v
}

func getEnvInt(key string, def int) int {
        v := os.Getenv(key)
        if v == "" {
                return def
        }
        n, err := strconv.Atoi(v)
        if err != nil {
                return def
        }
        return n
}

func getEnvInt64(key string, def int64) int64 {
        v := os.Getenv(key)
        if v == "" {
                return def
        }
        n, err := strconv.ParseInt(v, 10, 64)
        if err != nil {
                return def
        }
        return n
}

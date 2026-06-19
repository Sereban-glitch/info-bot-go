# info-bot-go

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/Go-1.19+-00ADD8?logo=go&logoColor=white)](https://golang.org)
[![Telegram](https://img.shields.io/badge/Telegram-Bot-26A5E4?logo=telegram&logoColor=white)](https://t.me/Infozaputbot)
[![Release](https://img.shields.io/github/v/release/Sereban-glitch/info-bot-go?display_name=tag)](https://github.com/Sereban-glitch/info-bot-go/releases)

**Freedom of Information (FOI) Bot** — Telegram-бот для отправки официальных запросов в государственные органы Украины через электронную почту.

## 🚀 Быстрый старт

### Вариант 1: Скачать готовый бинарник (рекомендуется)

```bash
# Linux amd64
wget https://github.com/Sereban-glitch/info-bot-go/releases/latest/download/info-bot-linux-amd64
chmod +x info-bot-linux-amd64
./info-bot-linux-amd64
```

### Вариант 2: Docker

```bash
docker run -d \
  --name info-bot \
  -p 8081:8081 \
  --env-file .env \
  ghcr.io/sereban-glitch/info-bot-go:latest
```

### Вариант 3: Сборка из исходников

```bash
git clone https://github.com/Sereban-glitch/info-bot-go.git
cd info-bot-go
cp .env.example .env  # заполнить TELEGRAM_BOT_TOKEN, GEMINI_API_KEY, и т.д.
go build -o info-bot .
./info-bot
```

### Переменные окружения

См. [`.env.example`](.env.example) для полного списка. Основные:

| Переменная | Описание |
|-----------|----------|
| `TELEGRAM_BOT_TOKEN` | Токен бота от @BotFather |
| `GEMINI_API_KEY` | API ключ Google Gemini |
| `GMAIL_USER` | Gmail для отправки запросов |
| `GMAIL_APP_PASSWORD` | Пароль приложения Gmail |

---

**Freedom of Information (FOI) Bot** — Telegram-бот для отправки официальных запросов в государственные органы Украины через электронную почту.

## 💡 Концепция: Экономия вашего времени

Больше не нужно составлять бумажные заявления, запечатывать конверты, идти на почту, стоять в очередях и платить за отправку писем.

Со **Smart Zapyt** весь бюрократический процесс сводится к одному действию: **просто запишите голосовое сообщение (это займет 30 секунд)**. AI-агент сам сформулирует юридически грамотный текст, найдёт правильный госорган в базе и официально отправит запрос. Сэкономленное время потратьте на жизнь, а не на очереди!


## Возможности

- **📝 Создание запросов** — отправка официальных писем-запросов в госорганы через SMTP
- **📬 Приём ответов** — мониторинг входящих писем через IMAP с защитой от дубликатов (кастомный флаг `$InfoBotProcessed`)
- **🤖 Gemini AI** — интеллектуальный поиск адресатов и анализ ответов
- **🗂️ Каталог органов** — встроенная база контактов госорганов Украины
- **🎤 Голосовой ввод** — поддержка голосовых сообщений через Telegram
- **🧪 Встроенное тестирование** — скрипт проверки SMTP/IMAP конвейера (`tools/test_mail`)

- **📱 Telegram Mini App** — встроенный веб-дашборд на Vercel (https://vidkrito-vercel.vercel.app/) для аналитики и шаблонов.

## 📸 Скриншоты и Интерфейс

**Smart Zapyt** — комплексное решение для автоматизации юридических запросов к госорганам Украины. Интерфейс включает дашборд с аналитикой, AI-генератор документов на базе Gemini и систему отслеживания статусов в режиме реального времени.

<div align="center">
  <img src="assets/gov_request_bot.jpg" width="80%" alt="Интерфейс чат-бота" />
  <br><i>Интерфейс чат-бота: уведомления и получение PDF-ответов</i><br><br>

  <img src="assets/smart_zapyt_mini_1.jpg" width="80%" alt="Главный экран мини-аппа" />
  <br><i>Главный экран мини-аппа: статистика пользователя и статусы</i><br><br>

</div>


## Быстрый старт

```bash
git clone https://github.com/Sereban-glitch/info-bot-go
cd info-bot-go
cp .env.example .env
# Заполните .env своими ключами
go run .
```

### Самотестирование почты

Перед первым запуском проверьте, что SMTP и IMAP работают:

```bash
source .env && go run ./tools/test_mail/
```

Тест отправляет письмо и проверяет его получение через IMAP, а также корректную работу флага `$InfoBotProcessed`.

## Переменные окружения

| Переменная | Описание |
|-----------|----------|
| `TELEGRAM_BOT_TOKEN` | Токен Telegram-бота (от @BotFather) |
| `GEMINI_API_KEY` | API ключ Google Gemini |
| `SMTP_HOST` | SMTP-сервер (smtp.gmail.com, smtp-relay.brevo.com) |
| `SMTP_USER` | Логин SMTP |
| `SMTP_PASSWORD` | Пароль SMTP |
| `SMTP_FROM_ADDR` | Адрес отправителя |
| `IMAP_HOST` | IMAP-сервер (imap.gmail.com) |
| `GMAIL_USER` | Логин IMAP |
| `GMAIL_APP_PASSWORD` | Пароль приложения IMAP |

## Архитектура

```
Telegram User → Telegram Bot API → info-bot-go (Go) → SMTP → Госорган
                    ↓                                         ↓
              Mini App (Vercel)                            IMAP ← Ответ
                    ↓                                         ↓
              Telegram WebApp Dashboard                 Telegram User
              vidkrito-vercel.vercel.app              (уведомление)
```

## Технологии

- **Go** — основной язык
- **Telebot v3** — Telegram Bot API
- **go-imap** — IMAP-клиент
- **Gemini API** — AI-функции
- **net/smtp** — отправка почты

## Статус

✅ Активно разрабатывается. Все IMAP-баги исправлены, дубликаты исключены.

## Лицензия

MIT

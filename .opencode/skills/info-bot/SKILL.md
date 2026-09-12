---
name: info-bot
description: Use when работа ведётся с Telegram-ботом Smart Zapyt (info-bot-go), сервером agrobot-prod (35.209.212.217), порталом dostup.org.ua (движок Alaveteli) — сборка/деплой/релиз бота, ошибки CSRF-токена, страница блокировки «First, did your other requests succeed?», классификация статусов ответов, обновление бэкапа на GitHub.
---

# Info-Bot (Smart Zapyt) — обслуживание и интеграция

## Карта проекта

- Исходники: `/home/archi/info-bot-src` (git, ветка `main`, remote `Sereban-glitch/info-bot-go`)
- Продакшн: systemd `info-bot.service` (User=archi, Restart=always, MemoryMax 250M), бинарь `/home/archi/info-bot/info-bot`, конфиг `/home/archi/info-bot/.env`, логи `/home/archi/info-bot/logs/bot.log`
- Мини-апп: Caddy → 127.0.0.1:8081, URL `https://35-209-212-217.sslip.io/`
- SSH-алиас: `agrobot-prod`. ВАЖНО: ssh-пользователь `u0_a566` НЕ имеет доступа к `/home/archi` (Permission denied) — все операции через `sudo`

## Сборка, тесты, деплой

Go 1.24.1 установлен в `/usr/local/go` (update-alternatives). **go.mod НЕ трогать** — правка на `go 1.19` для сборки больше не нужна (была костылём под старый Go).

```bash
# Сборка (флаги те же, что в CI — тогда sha256 совпадёт с GitHub-релизом)
sudo bash -c "cd /home/archi/info-bot-src && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags='-s -w' -o /tmp/info-bot-new ."

# Тесты
sudo bash -c "cd /home/archi/info-bot-src && go test ./..."

# Деплой
sudo systemctl stop info-bot.service
sudo cp /tmp/info-bot-new /home/archi/info-bot/info-bot
sudo chmod +x /home/archi/info-bot/info-bot
sudo systemctl start info-bot.service
sleep 2 && sudo systemctl is-active info-bot.service
sudo tail -15 /home/archi/info-bot/logs/bot.log   # ищем: module "classify" registered, без паник
```

Правила:
- **Часть файлов в репо принадлежит root** (classify.go, client.go, tools/) — редактирование/сборка от root, git-операции от archi (см. ниже).
- **НЕ создавать `info-bot.bak-*` при деплое** — это 30-бинарниковый мусор был удалён 2026-09-12; архив старых версий: `/home/archi/backups-info-bot-20260912.tar.gz` (root, 105 МБ).
- RAM-бюджет 1 ГБ: не запускать сборку параллельно с тяжёлыми сервисами.

## GitHub: пуш и «бэкап версии» (релиз)

Токен git есть ТОЛЬКО у пользователя `archi` (`/home/archi/.git-credentials`, helper store). Пуш от root -> `could not read Username`. **Всегда `sudo -u archi -H`**:

```bash
sudo -u archi -H git -C /home/archi/info-bot-src add -A
sudo -u archi -H git -C /home/archi/info-bot-src commit -m "feat: ..."
# при commit под sudo автор станет root — исправить: -c user.name="Sereban-glitch" -c user.email="admin@agrobot.local"
sudo -u archi -H git -C /home/archi/info-bot-src push origin main
sudo -u archi -H git -C /home/archi/info-bot-src tag -a v1.2.0 -m "v1.2.0"
sudo -u archi -H git -C /home/archi/info-bot-src push origin v1.2.0
# Проверка релиза (GitHub собирает 4 бинарника сам, ~2-3 мин):
curl -s https://api.github.com/repos/Sereban-glitch/info-bot-go/releases/tags/v1.2.0
```

Механика: `ci.yml` (пуш в main: vet+test+build на Go 1.24) и `release.yml` (тег `v*`: бинарники linux/darwin × amd64/arm64 в Releases) — это и есть бэкап версии на GitHub. Секреты не попадут: `.env`, `.dostup_session.json`, `*.bak*` в .gitignore.

## Интеграция с Alaveteli (dostup.org.ua) — добытые знания

Код: `internal/dostup/client.go`, `classify.go`, `suggest.go`; бот-часть: `internal/bot/handlers/dostup.go`, `dostupsync.go`, `classify.go`.

- **Сессия/CSRF**: cookie-сессия в `/home/archi/info-bot/.dostup_session.json`; токены достаются регэкспами `reTokenInput`/`reInputValue` со страниц. Редирект 302 после логина — НОРМАЛЬНО (не ошибка).
- **Блокировка аккаунта**: если старые ответы не классифицированы, Alaveteli блокирует подачу запроса страницей с текстом-маркером «First, did your other requests succeed?» (детект по этому тексту в HTML) → ошибка `ErrNeedsClassification` → алерт владельцу + сообщение пользователю, НЕ паника.
- **Классификация статуса**: `ReportStatus(slug, state)` — POST `https://dostup.org.ua/request/<slug>/classifications` с полями `authenticity_token`, `classification[described_state]`, `last_info_request_event_id=0`, `commit=Оновити статус`; успех = 301/302 (редирект).
- **Статусы**: `waiting_response`, `rejected`, `successful`, `partially_successful`, `not_held`, `gone_postal`, `error_message`, `requires_admin`, `user_withdrawn`.
- **Автоклассификация** (Шаг 3): `SuggestState(text)` — rule-based маркеры (rejected/successful/partially_successful/not_held); при неуверенности — AI-вердикт (Gemini) с маппингом только уверенных: `refusal→rejected`, `partial→partially_successful`, `substantive→successful`; спорное (brushoff/ack/unclear) → человеку. Правило: только уверенные случаи, кнопки исправления у пользователя всегда.
- **Тест-тул**: `tools/status-test` (или бинарь `/tmp/status-test`): `sudo /tmp/status-test <slug> <state>` — проверка ReportStatus на живом портале.

## Готовые маркеры для SuggestState (примеры)

rejected: «відмовлено», «на підставі ДСТУ», «не є розпорядником»; successful: «надаємо інформацію», «надсилаємо», «повідомляємо»; partially_successful: «частково», «не в повному обсязі»; not_held: «не володіє інформацією», «відсутня».
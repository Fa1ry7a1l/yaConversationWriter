# Умный помощник для конспектирования встреч

Go backend с Telegram-адаптером, memory/PostgreSQL repository, mock speech/LLM и ограниченным пулом фоновой обработки.

## Telegram-бот

1. Создайте бота через BotFather и сохраните токен только в environment:

   ```powershell
   $env:TELEGRAM_BOT_TOKEN = "<bot-token>"
   ```

2. Создайте локальную конфигурацию:

   ```powershell
   Copy-Item configs/config.example.yaml configs/config.local.yaml
   $env:APP_CONFIG_PATH = "configs/config.local.yaml"
   ```

3. В `configs/config.local.yaml` включите listener:

   ```yaml
   listeners:
     - name: primary
       type: telegram
       enabled: true
       token_env: TELEGRAM_BOT_TOKEN
   ```

4. Запустите приложение:

   ```powershell
   go run ./cmd/server
   ```

Telegram transport построен на `gopkg.in/telebot.v3`, работает через long polling и поддерживает личные чаты. Voice/audio до 20 МБ скачиваются ограниченно по размеру, встреча и задача создаются через application layer, а обработка выполняется worker pool в фоне. Можно настроить несколько listeners с разными `token_env`; они будут использовать общие сервисы и хранилище.

Команды:

- `/start` — регистрация;
- `list` — список встреч;
- `status <id>` — статус, даты и ошибка обработки;
- `get <id>` — полная транскрипция;
- `find <текст>` — поиск по своим встречам;
- `chat <id> <вопрос>` — вопрос mock LLM по завершённой встрече.

Команды принимаются как с `/`, так и без него. Speech и LLM остаются mock-реализациями; реальные AI API не используются.

## Локальный PostgreSQL

1. Создайте локальный env-файл:

   ```powershell
   Copy-Item deployments/.env.example deployments/.env
   ```

2. Запустите PostgreSQL:

   ```powershell
   docker compose --env-file deployments/.env -f deployments/docker-compose.yml up -d
   docker compose --env-file deployments/.env -f deployments/docker-compose.yml ps
   ```

3. В локальной конфигурации смените `storage.provider` на `postgres`:

   ```powershell
   if (-not (Test-Path configs/config.local.yaml)) { Copy-Item configs/config.example.yaml configs/config.local.yaml }
   $env:APP_CONFIG_PATH = "configs/config.local.yaml"
   $env:POSTGRES_PASSWORD = "change-me-for-local-development"
   ```

4. Примените миграции и запустите сервер:

   ```powershell
   go run ./cmd/migrate -direction up
   go run ./cmd/server
   ```

Откат первой миграции:

```powershell
go run ./cmd/migrate -direction down
```

## Тесты

Обычные тесты не требуют PostgreSQL:

```powershell
go test ./...
```

Интеграционные тесты запускаются отдельно против тестового Compose-профиля. Они применяют миграции и очищают таблицы, поэтому не направляйте `POSTGRES_TEST_DSN` на рабочую базу:

```powershell
docker compose --profile test --env-file deployments/.env -f deployments/docker-compose.yml up -d postgres-test
$env:POSTGRES_TEST_DSN = "postgres://meeting_notes:change-me-for-local-development@localhost:5433/meeting_notes_test?sslmode=disable"
go test -tags=integration ./internal/infrastructure/repository/postgres
```

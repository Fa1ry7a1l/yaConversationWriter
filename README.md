# Умный помощник для конспектирования встреч

Go backend с memory/PostgreSQL repository, mock speech/LLM и ограниченным пулом фоновой обработки. Пользовательский Telegram-адаптер подключается отдельным этапом.

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

3. Создайте локальную конфигурацию и смените `storage.provider` на `postgres`:

   ```powershell
   Copy-Item configs/config.example.yaml configs/config.local.yaml
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

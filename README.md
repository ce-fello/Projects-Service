# Projects Service

Микросервис приёма и отбора заявок на проекты от внешних инициаторов.

Сервис реализован на Go с использованием:
- `net/http` и стандартного `ServeMux`
- Postgres как основной БД
- JWT для аутентификации
- Docker Compose для локального запуска
- GitHub Actions для CI

API-контракт задания находится в [api.json](./api.json).

## Что реализовано

- Публичная подача внешней заявки на проект
- Просмотр справочника типов проектов
- Логин внутренних пользователей
- Административный просмотр, принятие и отклонение заявок
- Инварианты жизненного цикла заявки:
  - `PENDING -> ACCEPTED`
  - `PENDING -> REJECTED`
- Миграции базы данных
- Idempotent demo seed
- Unit и integration tests

## Бизнес-модель

### Роли

- `USER` может только логиниться
- `ADMIN` может просматривать заявки, список заявок, принимать и отклонять их
- Внешний инициатор подаёт заявку без авторизации

### Статусы заявки

- `PENDING`
- `ACCEPTED`
- `REJECTED`

Причина отказа существует только для `REJECTED` и дополнительно защищена constraint-ами в БД.

## Архитектура

Проект разделён на несколько слоёв:

- `cmd/service`
  - bootstrap приложения
  - создание HTTP-сервера
  - подключение к БД
  - запуск миграций и опционального demo seed

- `internal/domain`
  - базовые сущности домена
  - роли, actor, статусы, ошибки

- `internal/application`
  - use-cases сервиса
  - валидация входных данных
  - проверка прав доступа на application boundary
  - бизнес-правила переходов статусов

- `internal/platform/postgres`
  - открытие DB connection
  - транзакции
  - миграции
  - seed
  - SQL-репозитории

- `internal/platform/auth`
  - bcrypt password hashing
  - JWT generation/parsing

- `internal/transport/http`
  - HTTP handlers
  - middleware
  - request parsing
  - auth revalidation
  - request id и logging

### Ключевые инженерные решения

- Admin-права защищены на двух уровнях:
  - transport-level middleware валидирует токен и перечитывает пользователя из БД
  - application-layer use cases дополнительно требуют `domain.Actor` и сами проверяют роль
- Изменение статуса заявки выполняется транзакционно с `SELECT ... FOR UPDATE`
- Раннер миграций использует Postgres advisory lock, чтобы не гоняться при конкурентном старте
- Demo seed включается только через `ENABLE_DEMO_SEED=true`
- Для полного локального прогона тестов есть единая команда `make test`

## Структура данных

Основные таблицы:

- `users`
- `project_types`
- `external_applications`
- `schema_migrations`

Ключевые ограничения:

- `role IN ('ADMIN', 'USER')`
- `status IN ('PENDING', 'ACCEPTED', 'REJECTED')`
- согласованность `status` и `rejection_reason`
- foreign key на `project_types`

## Аутентификация и авторизация

### Логин

`POST /login` принимает логин и пароль и возвращает JWT.

### Передача токена

Токен передаётся в заголовке:

```http
X-API-TOKEN: <jwt>
```

### Актуальность прав

Даже после успешного логина admin-доступ не доверяет одному только JWT:
- пользователь перечитывается из БД на защищённых admin-ручках
- если пользователь удалён, сервис вернёт `401`
- если роль больше не `ADMIN`, сервис вернёт `403`

## HTTP API

Основные ручки:

- `POST /login`
- `GET /project/type`
- `POST /project/application/external`
- `GET /project/application/external/list`
- `GET /project/application/external/{applicationId}`
- `POST /project/application/external/{applicationId}/accept`
- `POST /project/application/external/{applicationId}/reject`

### Важные правила API

- `POST /project/application/external` не принимает `rejectedReason`
- причина отказа передаётся только в `POST /project/application/external/{applicationId}/reject`
- запросы ограничены по размеру body
- сервис возвращает `X-Request-ID` в response headers

## Локальный запуск

### Рекомендуемый способ

```bash
docker compose up -d
```

После старта сервис доступен на:

```text
http://localhost:8000
```

Compose поднимает:
- приложение
- Postgres

В `compose.yaml` уже включён demo seed.

### Тестовые пользователи

- `admin / admin123`
- `user / user123`

Это demo-учётки для локальной проверки тестового задания.

### Локальный запуск без Docker

Нужен доступный Postgres и переменные окружения:

```bash
export DATABASE_URL='postgres://postgres:postgres@127.0.0.1:5432/projects_service?sslmode=disable'
export JWT_SECRET='projects-service-secret'
export ENABLE_DEMO_SEED='true'
go run ./cmd/service
```

## Переменные окружения

- `HTTP_PORT`
  - порт HTTP-сервера
  - default: `8000`

- `DATABASE_URL`
  - строка подключения к Postgres
  - обязательна

- `JWT_SECRET`
  - секрет подписи JWT
  - обязательна

- `ENABLE_DEMO_SEED`
  - включает demo seed пользователей и типов проектов
  - default: `false`

## Тестирование

### Полный локальный прогон

```bash
make test
```

Что делает команда:
- поднимает `postgres` через Docker Compose
- запускает unit + integration tests
- прогоняет suite последовательно (`go test -p 1 ./...`), чтобы избежать гонок за общей тестовой БД
- останавливает контейнер после завершения

### Быстрый прогон без Docker

```bash
make test-unit
```

### Только integration tests

```bash
make test-integration
```

При необходимости можно переопределить БД:

```bash
make test TEST_DATABASE_URL='postgres://postgres:postgres@127.0.0.1:5432/projects_service?sslmode=disable'
```

## CI

В репозитории настроен GitHub Actions pipeline:
- workflow запускается на `push`
- поднимает Postgres service container
- выполняет тесты через `make test-integration`

Файл workflow: [`.github/workflows/ci.yaml`](./.github/workflows/ci.yaml)

## Наблюдаемость и hardening

Что уже есть:
- request logging через `slog`
- `X-Request-ID`
- логирование `status`, `response_bytes`, `duration`, `user_id`, `error_category`
- `ReadHeaderTimeout`, `ReadTimeout`, `WriteTimeout`, `IdleTimeout`
- ограничение размера request body


## Проверка сценария руками

1. Запустить сервис:

```bash
docker compose up -d
```

2. Получить токен администратора:

```bash
curl -sS -X POST http://localhost:8000/login \
  -H 'Content-Type: application/json' \
  -d '{"login":"admin","password":"admin123"}'
```

3. Получить типы проектов:

```bash
curl -sS http://localhost:8000/project/type
```

4. Подать заявку:

```bash
curl -sS -X POST http://localhost:8000/project/application/external \
  -H 'Content-Type: application/json' \
  -d '{
    "fullName":"Ivan Ivanov",
    "email":"ivan@example.com",
    "phone":"+7 (999) 123-45-67",
    "organisationName":"ACME",
    "organisationUrl":"https://acme.test",
    "projectName":"New Venture",
    "typeId":1,
    "expectedResults":"Launch MVP",
    "isPayed":true,
    "additionalInformation":"Important details"
  }'
```

5. Посмотреть список заявок:

```bash
curl -sS 'http://localhost:8000/project/application/external/list?active=true' \
  -H 'X-API-TOKEN: <token>'
```

6. Принять или отклонить заявку:

```bash
curl -sS -X POST http://localhost:8000/project/application/external/1/accept \
  -H 'X-API-TOKEN: <token>'
```

или

```bash
curl -sS -X POST http://localhost:8000/project/application/external/1/reject \
  -H 'Content-Type: application/json' \
  -H 'X-API-TOKEN: <token>' \
  -d '{"reason":"Out of scope"}'
```

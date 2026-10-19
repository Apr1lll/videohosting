# Videohosting — бэкенд видеохостинга на Go (микросервисы)

Курсовая работа: видеохостинг «Кадр». Пользователи регистрируются, загружают и смотрят видео (с перемоткой), оставляют комментарии, ставят лайки и подписываются на каналы.

Проект состоит из бэкенда (4 микросервиса на Go) и фронтенда (одностраничное приложение на чистом HTML/CSS/JS, без сборки и фреймворков).

Как залить на GitHub и развернуть на VPS — пошагово в [DEPLOY.md](DEPLOY.md).

## Архитектура

```
                    ┌──────────────────────┐
  браузер ──HTTP─▶  │  api-gateway :8080   │  JWT, маршрутизация, rate limit, раздача фронтенда
                    └───┬────────┬────────┬┘
                        │        │        │
              ┌─────────▼──┐ ┌───▼──────────┐ ┌▼────────────────────┐
              │auth-service│ │video-service │ │interaction-service  │
              │   :8081    │ │    :8082     │◀┼▶      :8083         │
              └─────┬──────┘ └──┬───────┬───┘ └──┬──────────┬───────┘
                    │           │       │        │          │
               auth_db     video_db   Redis   interaction_db  Redis
                         + volume с файлами
```

| Сервис | Ответственность | Хранилище |
|---|---|---|
| **api-gateway** | Единая точка входа. Отдаёт фронтенд (всё, что не API, — файлы из `frontend/`). Проверяет JWT и передаёт `X-User-ID` / `X-Username` в сервисы. Закрывает `/internal/*`. Rate limit (300 запросов/мин с IP), CORS, `X-Request-ID`, сводный `/health` | — |
| **auth-service** | Регистрация, вход (bcrypt + JWT HS256), профиль | `auth_db.users` |
| **video-service** | Загрузка (mp4/webm/mov/mkv, до 500 МБ), поиск, карточка, стриминг с `Range`, удаление, топ популярных | `video_db.videos`, файлы в volume, Redis |
| **interaction-service** | Комментарии, лайки/дизлайки, подписки | `interaction_db.*`, Redis |

**Принципы:**
- **Database per service**: у каждого сервиса своя БД, чужие таблицы он не читает.
- **Межсервисное общение по REST:**
  - interaction → video: проверка, что видео существует;
  - interaction → auth: проверка, что канал существует;
  - video → interaction: удаление комментариев и лайков удалённого видео.
- **Слоистая структура** внутри сервиса: `handler` (HTTP) → `service` (бизнес-логика) → `repository` (SQL). Сервис зависит от интерфейсов, а не от конкретных реализаций.

**Как используется Redis:**
- карточки видео кешируются на 5 минут;
- топ популярных кешируется на 1 минуту;
- просмотры копятся в Redis (`INCR`) и раз в 30 секунд пачкой записываются в Postgres, чтобы не делать `UPDATE` на каждый просмотр;
- счётчики лайков и дизлайков кешируются и сбрасываются при каждом изменении.

## Стек
Go 1.23+, PostgreSQL 16, Redis 7, Docker Compose, golang-migrate.

Библиотеки: `jackc/pgx/v5`, `redis/go-redis/v9`, `golang.org/x/crypto/bcrypt`, `golang-jwt/jwt/v5`. Роутинг сделан на стандартном `net/http` (шаблоны вида `GET /videos/{id}` из Go 1.22).

## Запуск

```bash
docker compose up --build
```

Postgres создаёт три базы (`postgres/init.sql`), затем контейнеры `migrate-*` применяют миграции, и только после этого стартуют сервисы. Сайт и API доступны на `http://localhost:8080`.

Пароли и порт задаются в `.env` (образец — `.env.example`).

Сквозной тест (27 проверок через curl):
```bash
./scripts/smoke_test.sh
```

Остановить: `docker compose down`. Остановить и удалить данные: `docker compose down -v`.

## API

Запросы, отмеченные 🔒, требуют заголовок `Authorization: Bearer <token>`.

| Метод | Путь | Описание |
|---|---|---|
| POST | `/auth/register` | `{username, email, password}` |
| POST | `/auth/login` | `{email, password}` → `{token, expires_at, user}` |
| GET 🔒 | `/users/me` | свой профиль |
| PATCH 🔒 | `/users/me` | `{bio}` |
| GET | `/users/{id}` | публичный профиль |
| POST 🔒 | `/videos` | multipart: `title`, `description`, `file` |
| GET | `/videos?search=&author_id=&page=&limit=` | список и поиск |
| GET | `/videos/popular` | топ-10 по просмотрам |
| GET | `/videos/{id}` | карточка видео |
| GET | `/videos/{id}/stream` | видеофайл (поддерживает `Range`) |
| DELETE 🔒 | `/videos/{id}` | удалить своё видео |
| POST 🔒 | `/videos/{id}/comments` | `{text}` |
| GET | `/videos/{id}/comments?page=&limit=` | комментарии |
| DELETE 🔒 | `/comments/{id}` | удалить свой комментарий |
| POST 🔒 | `/videos/{id}/like`, `/dislike` | повторное нажатие снимает реакцию |
| GET | `/videos/{id}/reactions` | лайки, дизлайки и своя реакция |
| POST / DELETE 🔒 | `/channels/{id}/subscribe` | подписаться / отписаться |
| GET | `/channels/{id}/subscribers` | число подписчиков |
| GET 🔒 | `/users/me/subscriptions` | мои подписки |
| GET | `/health` | состояние всех сервисов |

### Примеры

```bash
curl -X POST localhost:8080/auth/register -H 'Content-Type: application/json' \
  -d '{"username":"alice","email":"alice@mail.ru","password":"secret123"}'

TOKEN=$(curl -s -X POST localhost:8080/auth/login -H 'Content-Type: application/json' \
  -d '{"email":"alice@mail.ru","password":"secret123"}' | sed 's/.*"token":"\([^"]*\)".*/\1/')

curl -X POST localhost:8080/videos -H "Authorization: Bearer $TOKEN" \
  -F "title=Котики" -F "description=смешные" -F "file=@cats.mp4"

curl localhost:8080/videos?search=кот
```

Видео можно открыть прямо в браузере: `http://localhost:8080/videos/1/stream`. Перемотка работает.

Ошибки всегда возвращаются в формате `{"error": "описание"}` с соответствующим HTTP-кодом: 400, 401, 403, 404, 409, 413, 429, 502.

## Фронтенд

Папка `frontend/`: `index.html`, `styles.css`, `app.js`. Hash-роутинг (`#/video/5`, `#/channel/2`), поэтому адреса страниц не пересекаются с адресами API.

| Страница | Что умеет |
|---|---|
| Главная | новые видео с подгрузкой, вкладка «Популярное» |
| Поиск | поиск по названию и описанию |
| Видео | плеер с перемоткой, лайки/дизлайки, подписка на автора, комментарии, другие видео автора, удаление своего видео |
| Канал | профиль, число подписчиков, видео автора, редактирование описания своего канала |
| Подписки | лента свежих видео с каналов, на которые вы подписаны |
| Загрузка | выбор или перетаскивание файла, индикатор прогресса |
| Вход / регистрация | JWT хранится в `localStorage` |

У видео нет превью, поэтому карточки нарисованы как кадр плёнки: название набрано крупно, цвет постоянный для каждого видео.

## Структура проекта

```
├── docker-compose.yml
├── frontend/                    сайт (раздаётся api-gateway)
├── postgres/init.sql            создание баз
├── scripts/smoke_test.sh        сквозной тест
├── api-gateway/
│   └── internal/{auth,config,gateway}
├── auth-service/
│   ├── cmd/server/main.go
│   ├── internal/{config,database,handler,httpx,model,repository,service}
│   └── migrations/
├── video-service/               + cache/, storage/, client/
└── interaction-service/         + cache/, client/
```

## Безопасность
- Пароли хранятся как bcrypt-хеш. При входе с несуществующим email тоже выполняется сравнение хеша, чтобы по времени ответа нельзя было определить, зарегистрирован ли email.
- Gateway удаляет присланные клиентом заголовки `X-User-ID` и `X-Username`, поэтому подделать пользователя нельзя.
- Пути `/internal/*` закрыты снаружи.
- Видеофайлы сохраняются под случайными именами; путь к файлу от клиента не зависит.
- Размер загрузки ограничен, на запросы действует rate limit.

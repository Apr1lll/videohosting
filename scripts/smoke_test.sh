#!/usr/bin/env bash
# Сквозной тест всего API через gateway. Запуск: ./scripts/smoke_test.sh
# Требуется: запущенный docker compose, curl.
set -euo pipefail

API=${API:-http://localhost:8080}
SUFFIX=$RANDOM
PASS=0

check() { # check <название> <ожидаемый код> <фактический код>
  if [ "$2" == "$3" ]; then
    echo "  OK   $1 ($3)"; PASS=$((PASS+1))
  else
    echo "  FAIL $1: ожидали $2, получили $3"; exit 1
  fi
}
json_field() { grep -o "\"$1\":[^,}]*" | head -1 | cut -d: -f2- | tr -d '"'; }

echo "== Регистрация и вход"
code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$API/auth/register" -H 'Content-Type: application/json' \
  -d "{\"username\":\"alice$SUFFIX\",\"email\":\"alice$SUFFIX@mail.ru\",\"password\":\"secret123\"}")
check "регистрация alice" 201 "$code"

resp=$(curl -s -X POST "$API/auth/register" -H 'Content-Type: application/json' \
  -d "{\"username\":\"bob$SUFFIX\",\"email\":\"bob$SUFFIX@mail.ru\",\"password\":\"secret123\"}")
BOB_ID=$(echo "$resp" | json_field id)

code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$API/auth/register" -H 'Content-Type: application/json' \
  -d "{\"username\":\"alice$SUFFIX\",\"email\":\"alice$SUFFIX@mail.ru\",\"password\":\"secret123\"}")
check "повторная регистрация" 409 "$code"

code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$API/auth/login" -H 'Content-Type: application/json' \
  -d "{\"email\":\"alice$SUFFIX@mail.ru\",\"password\":\"wrong\"}")
check "вход с неверным паролем" 401 "$code"

TOKEN=$(curl -s -X POST "$API/auth/login" -H 'Content-Type: application/json' \
  -d "{\"email\":\"alice$SUFFIX@mail.ru\",\"password\":\"secret123\"}" | json_field token)
BOB_TOKEN=$(curl -s -X POST "$API/auth/login" -H 'Content-Type: application/json' \
  -d "{\"email\":\"bob$SUFFIX@mail.ru\",\"password\":\"secret123\"}" | json_field token)
AUTH="Authorization: Bearer $TOKEN"

code=$(curl -s -o /dev/null -w '%{http_code}' "$API/users/me" -H "$AUTH")
check "GET /users/me" 200 "$code"
code=$(curl -s -o /dev/null -w '%{http_code}' "$API/users/me")
check "GET /users/me без токена" 401 "$code"

echo "== Видео"
TMP=$(mktemp -d)
head -c 300000 /dev/urandom > "$TMP/test.mp4"
resp=$(curl -s -X POST "$API/videos" -H "$AUTH" -F "title=Мой первый ролик" -F "description=тест" -F "file=@$TMP/test.mp4")
VIDEO_ID=$(echo "$resp" | json_field id)
[ -n "$VIDEO_ID" ] && check "загрузка видео" ok ok || { echo "$resp"; exit 1; }

echo "text" > "$TMP/bad.txt"
code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$API/videos" -H "$AUTH" -F "title=x" -F "file=@$TMP/bad.txt")
check "загрузка неподдерживаемого формата" 400 "$code"

code=$(curl -s -o /dev/null -w '%{http_code}' "$API/videos/$VIDEO_ID")
check "карточка видео" 200 "$code"
code=$(curl -s -o /dev/null -w '%{http_code}' "$API/videos?search=первый")
check "поиск видео" 200 "$code"
code=$(curl -s -o "$TMP/out.mp4" -w '%{http_code}' "$API/videos/$VIDEO_ID/stream")
check "стриминг целиком" 200 "$code"
cmp -s "$TMP/test.mp4" "$TMP/out.mp4" && check "файл совпадает с загруженным" ok ok
code=$(curl -s -o /dev/null -w '%{http_code}' -H 'Range: bytes=1000-1999' "$API/videos/$VIDEO_ID/stream")
check "Range-запрос (перемотка)" 206 "$code"
code=$(curl -s -o /dev/null -w '%{http_code}' "$API/videos/popular")
check "популярные видео" 200 "$code"

echo "== Комментарии и реакции"
resp=$(curl -s -X POST "$API/videos/$VIDEO_ID/comments" -H "Authorization: Bearer $BOB_TOKEN" \
  -H 'Content-Type: application/json' -d '{"text":"Классное видео!"}')
COMMENT_ID=$(echo "$resp" | json_field id)
[ -n "$COMMENT_ID" ] && check "комментарий" ok ok || { echo "$resp"; exit 1; }
code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$API/videos/999999/comments" -H "$AUTH" \
  -H 'Content-Type: application/json' -d '{"text":"?"}')
check "комментарий к несуществующему видео" 404 "$code"
code=$(curl -s -o /dev/null -w '%{http_code}' -X DELETE "$API/comments/$COMMENT_ID" -H "$AUTH")
check "удаление чужого комментария" 403 "$code"
code=$(curl -s -o /dev/null -w '%{http_code}' "$API/videos/$VIDEO_ID/comments")
check "список комментариев" 200 "$code"

likes=$(curl -s -X POST "$API/videos/$VIDEO_ID/like" -H "Authorization: Bearer $BOB_TOKEN" | json_field likes)
check "лайк" 1 "$likes"
likes=$(curl -s -X POST "$API/videos/$VIDEO_ID/like" -H "Authorization: Bearer $BOB_TOKEN" | json_field likes)
check "повторный лайк снимает его" 0 "$likes"

echo "== Подписки"
code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$API/channels/$BOB_ID/subscribe" -H "$AUTH")
check "подписка на канал" 200 "$code"
subs=$(curl -s "$API/channels/$BOB_ID/subscribers" | json_field subscribers)
check "число подписчиков" 1 "$subs"
code=$(curl -s -o /dev/null -w '%{http_code}' "$API/users/me/subscriptions" -H "$AUTH")
check "мои подписки" 200 "$code"

echo "== Безопасность"
code=$(curl -s -o /dev/null -w '%{http_code}' -X DELETE "$API/internal/videos/$VIDEO_ID")
check "внутренний эндпоинт недоступен снаружи" 404 "$code"
code=$(curl -s -o /dev/null -w '%{http_code}' "$API/users/me" -H "Authorization: Bearer fake.token.here")
check "поддельный токен" 401 "$code"
code=$(curl -s -o /dev/null -w '%{http_code}' -X DELETE "$API/videos/$VIDEO_ID" -H "Authorization: Bearer $BOB_TOKEN")
check "удаление чужого видео" 403 "$code"

echo "== Превью, аватарки, редактирование"
# Минимальный JPEG: серверу достаточно сигнатуры FF D8 FF, чтобы распознать тип картинки.
{ printf '\xff\xd8\xff\xe0\x00\x10JFIF\x00'; head -c 2000 /dev/urandom; } > "$TMP/pic.jpg"
resp=$(curl -s -X POST "$API/videos" -H "$AUTH" -F "title=Видео с превью про котиков" -F "duration=125" \
  -F "thumbnail=@$TMP/pic.jpg" -F "file=@$TMP/test.mp4")
V2=$(echo "$resp" | json_field id)
dur=$(echo "$resp" | json_field duration_seconds)
check "загрузка с превью и длительностью" 125 "$dur"
code=$(curl -s -o /dev/null -w '%{http_code}' "$API/videos/$V2/thumbnail")
check "превью отдаётся" 200 "$code"
code=$(curl -s -o /dev/null -w '%{http_code}' -X PUT "$API/videos/$V2/thumbnail" -H "$AUTH" -F "thumbnail=@$TMP/bad.txt")
check "превью не картинкой отклоняется" 400 "$code"
title=$(curl -s -X PATCH "$API/videos/$V2" -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{"title":"Новое название про котиков","description":"обновили"}' | json_field title)
check "редактирование видео" "Новое название про котиков" "$title"
code=$(curl -s -o /dev/null -w '%{http_code}' -X PATCH "$API/videos/$V2" -H "Authorization: Bearer $BOB_TOKEN" \
  -H 'Content-Type: application/json' -d '{"title":"взлом","description":""}')
check "чужое видео редактировать нельзя" 403 "$code"
ctype=$(curl -s -o /dev/null -w '%{content_type}' "$API/users/$BOB_ID/avatar")
check "аватарка по умолчанию (SVG)" "image/svg+xml" "$ctype"
has=$(curl -s -X PUT "$API/users/me/avatar" -H "$AUTH" -F "avatar=@$TMP/pic.jpg" | json_field has_avatar)
check "загрузка аватарки" true "$has"

echo "== Рекомендации"
curl -s -o /dev/null -X POST "$API/videos" -H "Authorization: Bearer $BOB_TOKEN" -F "title=Смешные котики компиляция" -F "file=@$TMP/test.mp4"
curl -s -o /dev/null -X POST "$API/videos" -H "Authorization: Bearer $BOB_TOKEN" -F "title=Ремонт кухни своими руками" -F "file=@$TMP/test.mp4"
rec=$(curl -s "$API/videos/$V2/recommendations?limit=5")
first=$(echo "$rec" | grep -o '"title":"[^"]*"' | head -1 | cut -d'"' -f4)
second=$(echo "$rec" | grep -o '"title":"[^"]*"' | sed -n 2p | cut -d'"' -f4)
check "1-е место: другое видео этого же автора" "Мой первый ролик" "$first"
check "2-е место: похожее по теме (котики)" "Смешные котики компиляция" "$second"

echo "== Ответы и редактирование комментариев"
PARENT=$(curl -s -X POST "$API/videos/$V2/comments" -H "$AUTH" -H 'Content-Type: application/json' -d '{"text":"Корневой"}' | json_field id)
curl -s -o /dev/null -X POST "$API/videos/$V2/comments" -H "Authorization: Bearer $BOB_TOKEN" -H 'Content-Type: application/json' -d "{\"text\":\"Ответ\",\"parent_id\":$PARENT}"
n=$(curl -s "$API/videos/$V2/comments" | json_field reply_count)
check "у комментария 1 ответ" 1 "$n"
code=$(curl -s -o /dev/null -w '%{http_code}' "$API/comments/$PARENT/replies")
check "список ответов" 200 "$code"
edited=$(curl -s -X PATCH "$API/comments/$PARENT" -H "$AUTH" -H 'Content-Type: application/json' -d '{"text":"Исправлено"}' | json_field text)
check "редактирование своего комментария" "Исправлено" "$edited"

echo "== Подписчики и подписка на пустой канал"
resp=$(curl -s -X POST "$API/auth/register" -H 'Content-Type: application/json' \
  -d "{\"username\":\"empty$SUFFIX\",\"email\":\"empty$SUFFIX@mail.ru\",\"password\":\"secret123\"}")
EMPTY_ID=$(echo "$resp" | json_field id)
code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$API/channels/$EMPTY_ID/subscribe" -H "$AUTH")
check "подписка на канал без видео" 200 "$code"
subs=$(curl -s "$API/users/me/subscribers" -H "Authorization: Bearer $BOB_TOKEN" | grep -o "alice$SUFFIX" | head -1)
check "bob видит подписчика alice" "alice$SUFFIX" "$subs"
code=$(curl -s -o /dev/null -w '%{http_code}' "$API/admin/users" -H "$AUTH")
check "обычному пользователю админка закрыта" 403 "$code"

if [ -n "${ADMIN_EMAIL:-}" ] && [ -n "${ADMIN_PASSWORD:-}" ]; then
  echo "== Админ-панель"
  ADM=$(curl -s -X POST "$API/auth/login" -H 'Content-Type: application/json' \
    -d "{\"email\":\"$ADMIN_EMAIL\",\"password\":\"$ADMIN_PASSWORD\"}" | json_field token)
  ADM="Authorization: Bearer $ADM"
  code=$(curl -s -o /dev/null -w '%{http_code}' "$API/admin/users?search=alice$SUFFIX" -H "$ADM")
  check "админ видит пользователей" 200 "$code"
  code=$(curl -s -o /dev/null -w '%{http_code}' "$API/admin/videos/stats" -H "$ADM")
  check "статистика видео" 200 "$code"
  code=$(curl -s -o /dev/null -w '%{http_code}' -X PATCH "$API/comments/$PARENT" -H "$ADM" -H 'Content-Type: application/json' -d '{"text":"Отмодерировано"}')
  check "админ редактирует чужой комментарий" 200 "$code"
  code=$(curl -s -o /dev/null -w '%{http_code}' -X DELETE "$API/admin/users/$BOB_ID" -H "$ADM")
  check "админ удаляет пользователя bob" 204 "$code"
  left=$(curl -s "$API/videos?search=Смешные%20котики" | json_field total)
  check "видео bob удалены вместе с ним" 0 "$left"
else
  echo "  (админ-проверки пропущены: задайте ADMIN_EMAIL и ADMIN_PASSWORD, чтобы их запустить)"
fi

echo "== Удаление"
code=$(curl -s -o /dev/null -w '%{http_code}' -X DELETE "$API/videos/$VIDEO_ID" -H "$AUTH")
check "удаление своего видео" 204 "$code"
code=$(curl -s -o /dev/null -w '%{http_code}' "$API/videos/$VIDEO_ID")
check "видео больше не найдено" 404 "$code"
curl -s -o /dev/null -X DELETE "$API/videos/$V2" -H "$AUTH"

rm -rf "$TMP"
echo
echo "Все проверки пройдены: $PASS"

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

echo "== Удаление"
code=$(curl -s -o /dev/null -w '%{http_code}' -X DELETE "$API/videos/$VIDEO_ID" -H "$AUTH")
check "удаление своего видео" 204 "$code"
code=$(curl -s -o /dev/null -w '%{http_code}' "$API/videos/$VIDEO_ID")
check "видео больше не найдено" 404 "$code"

rm -rf "$TMP"
echo
echo "Все проверки пройдены: $PASS"

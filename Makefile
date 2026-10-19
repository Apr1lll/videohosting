.PHONY: up down logs build test clean

up:          ## собрать и запустить всё
	docker compose up --build -d

down:        ## остановить
	docker compose down

logs:        ## логи всех сервисов
	docker compose logs -f auth-service video-service interaction-service api-gateway

build:       ## проверить, что все сервисы компилируются
	cd auth-service && go build ./...
	cd video-service && go build ./...
	cd interaction-service && go build ./...
	cd api-gateway && go build ./...

test:        ## сквозной тест через curl (нужен запущенный docker compose)
	./scripts/smoke_test.sh

clean:       ## удалить контейнеры вместе с данными
	docker compose down -v

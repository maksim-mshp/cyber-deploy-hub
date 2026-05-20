COMPOSE ?= docker compose
DATABASE_URL ?= postgres://cyber:change-me@localhost:5432/cyber_deploy_hub?sslmode=disable

.PHONY: up up-detached down logs ps migrate backend-test frontend-lint frontend-build test compose-config

up:
	$(COMPOSE) up --build

up-detached:
	$(COMPOSE) up --build -d

down:
	$(COMPOSE) down --remove-orphans

logs:
	$(COMPOSE) logs -f

ps:
	$(COMPOSE) ps

migrate:
	cd backend && DATABASE_URL="$(DATABASE_URL)" go run ./cmd/migrate -path file://migrations -action up

backend-test:
	cd backend && go test ./...

frontend-lint:
	cd frontend && npm run lint

frontend-build:
	cd frontend && npm run build

test: backend-test frontend-lint frontend-build

compose-config:
	$(COMPOSE) config

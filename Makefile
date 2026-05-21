COMPOSE ?= docker compose
DATABASE_URL ?= postgres://cyber:change-me@localhost:5432/cyber_deploy_hub?sslmode=disable
GOLANGCI_LINT ?= go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.7.2
GOSEC ?= go run github.com/securego/gosec/v2/cmd/gosec@v2.22.10
GOVULNCHECK ?= go run golang.org/x/vuln/cmd/govulncheck@v1.1.4
TRIVY ?= docker run --rm -v /var/run/docker.sock:/var/run/docker.sock aquasec/trivy:0.67.2

.PHONY: up up-detached down logs ps migrate backend-test backend-lint backend-gosec backend-govulncheck backend-security frontend-lint frontend-build frontend-audit secret-scan trivy-images test security compose-config

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

backend-lint:
	cd backend && $(GOLANGCI_LINT) run ./...

backend-gosec:
	cd backend && $(GOSEC) ./...

backend-govulncheck:
	cd backend && $(GOVULNCHECK) ./...

backend-security: backend-gosec backend-govulncheck

frontend-lint:
	cd frontend && npm run lint

frontend-build:
	cd frontend && npm run build

frontend-audit:
	cd frontend && npm audit --audit-level=high

secret-scan:
	./scripts/secret-scan.sh

trivy-images:
	$(TRIVY) image --severity HIGH,CRITICAL --exit-code 1 cyber-deploy-hub-lms-gateway-service:latest
	$(TRIVY) image --severity HIGH,CRITICAL --exit-code 1 cyber-deploy-hub-web:latest

test: backend-test frontend-lint frontend-build

security: backend-security frontend-audit secret-scan

compose-config:
	$(COMPOSE) config

# ---------------------------
# Secure Messenger Makefile
# ---------------------------

COMPOSE = docker compose

.PHONY: dev up start stop rebuild down clean logs build restart ps health test lint

# Foreground dev (rebuild if needed)
dev:
	$(COMPOSE) up --build

# Alias
up:
	$(COMPOSE) up --build

# Background start
start:
	$(COMPOSE) up -d --build

stop:
	$(COMPOSE) stop

# Rebuild images + run foreground
rebuild:
	$(COMPOSE) up --build

down:
	$(COMPOSE) down

# Nuke volumes (resets Postgres data)
clean:
	$(COMPOSE) down -v

logs:
	$(COMPOSE) logs -f --tail=200

build:
	$(COMPOSE) build

restart:
	$(COMPOSE) down
	$(COMPOSE) up -d --build

ps:
	$(COMPOSE) ps

health:
	curl -fsS http://localhost:8080/health/ready | cat

test:
	go test ./... -count=1

lint:
	golangci-lint run ./...

fmt:
	go fmt ./...

tidy:
	go mod tidy

fix: fmt tidy

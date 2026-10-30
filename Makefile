COMPOSE ?= docker compose

.DEFAULT_GOAL := help
.PHONY: help env up down restart ps logs user ca-cert migrate test web check-env

help: ## Tampilkan daftar perintah
	@echo "Pemakaian: make env -> make up. Restart: make down && make up"
	@echo
	@awk 'BEGIN {FS = ":.*## "} /^[a-z-]+:.*## / {printf "  make %-9s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

env: ## Buat .env (IP server + secret acak). IP manual: make env IP=10.0.0.5
	@sh scripts/gen-env.sh "$(IP)"

up: check-env ## Build image, migrasi database, jalankan semua service
	$(COMPOSE) build
	$(COMPOSE) up -d --wait postgres redis jaeger
	$(COMPOSE) run --rm --no-deps --entrypoint /app/migrate app up
	$(COMPOSE) up -d --wait
	@site=$$(sed -n 's/^SITE_ADDRESS=//p' .env); \
	echo; \
	echo "Aplikasi : https://$$site  (sertifikat CA internal Caddy; lihat 'make ca-cert')"; \
	echo "Jaeger UI: http://$$site:16686"; \
	echo "Buat akun: make user EMAIL=admin@example.com NAME=Admin ROLE=admin"

down: ## Hentikan semua service (data Postgres/Redis tetap tersimpan)
	$(COMPOSE) down

restart: ## Sama dengan: make down && make up
	@$(MAKE) --no-print-directory down
	@$(MAKE) --no-print-directory up

ps: ## Status dan health semua service
	$(COMPOSE) ps

logs: ## Ikuti log (satu service: make logs S=app)
	$(COMPOSE) logs -f --tail=100 $(S)

user: check-env ## Buat akun: make user EMAIL=a@b.com NAME="Nama" ROLE=client|admin
	@test -n "$(EMAIL)" && test -n "$(NAME)" || { echo "Wajib: make user EMAIL=... NAME=... [ROLE=client|admin]"; exit 1; }
	$(COMPOSE) exec app /app/create-user --email "$(EMAIL)" --name "$(NAME)" --role "$(or $(ROLE),client)"

ca-cert: ## Salin root CA Caddy ke ./caddy-root.crt (import ke browser agar tanpa peringatan)
	$(COMPOSE) cp caddy:/data/caddy/pki/authorities/local/root.crt ./caddy-root.crt
	@echo "Import caddy-root.crt sebagai Trusted Root CA di browser/OS."

migrate: check-env ## Jalankan migrasi database saja
	$(COMPOSE) run --rm --entrypoint /app/migrate app up

test: ## Jalankan unit test Go
	go test ./...

web: ## Build otel.js secara lokal (butuh Node), untuk menjalankan server tanpa Docker
	cd web && npm ci --no-audit --no-fund && npm run build
	cp web/dist/otel.js internal/templates/static/otel.js

check-env:
	@test -f .env || { echo ".env belum ada. Jalankan dulu: make env"; exit 1; }

# PatiKlinik — sık kullanılan komutlar. Tüm hedefler yalnız Docker gerektirir (yerel Go/Node gerekmez),
# test ve lint hedefleri dışında.

COMPOSE := docker compose

.PHONY: help env up down logs ps verify reset-demo load test test-unit lint fe-build check e2e clean

help: ## Komutları listele
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

env: ## .env yoksa .env.example'dan oluştur
	@test -f .env || cp .env.example .env

up: env ## Tüm servisleri derle ve başlat (frontend :3000, API :8080)
	$(COMPOSE) up --build -d
	@echo "Arayüz: http://localhost:3000  ·  API: http://localhost:8080  ·  Swagger: http://localhost:8080/docs"

down: ## Servisleri durdur (veri korunur)
	$(COMPOSE) down

logs: ## Logları izle
	$(COMPOSE) logs -f --tail=100

ps: ## Servis durumları
	$(COMPOSE) ps

verify: env ## Her özelliği ve zorlu görevi uçtan uca doğrula, PASS/FAIL tablosu bas
	$(COMPOSE) --profile verify run --rm --build verify

reset-demo: env ## Verileri ilk seed haline döndür (aynı SEED → aynı veri)
	$(COMPOSE) stop api simulator
	$(COMPOSE) run --rm --no-deps simulator reset
	$(COMPOSE) up -d api simulator frontend
	@echo "Demo verisi sıfırlandı."

load: env ## k6 yük testi (acil vaka + rezervasyon + doz hesabı), özet load/results/summary.json
	$(COMPOSE) --profile load run --rm k6

test: ## Go birim + entegrasyon testleri (Docker gerekir: testcontainers)
	cd backend && go test -tags integration -race -count=1 ./...

test-unit: ## Yalnız birim testleri (Docker gerekmez)
	cd backend && go test -count=1 ./...

lint: ## golangci-lint (Docker ile)
	MSYS_NO_PATHCONV=1 docker run --rm -v "$(CURDIR)/backend:/app" -w /app golangci/golangci-lint:v2.15.0 golangci-lint run ./...

fe-build: ## Frontend tip kontrolü ve derleme
	cd frontend && npm ci && npm run build

check: lint test-unit ## Push öncesi hızlı kontrol: lint + birim testler + frontend tip kontrolü
	cd frontend && npm run typecheck

e2e: ## Arayüz uçtan uca testleri (Playwright; sistem make up ile çalışıyor olmalı)
	cd e2e && npm ci --no-audit --no-fund && npx playwright install chromium && npx playwright test

clean: ## Servisleri ve veritabanı hacmini sil
	$(COMPOSE) down -v

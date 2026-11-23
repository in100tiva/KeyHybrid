SHELL := /bin/bash
GO ?= go
PNPM ?= pnpm

.DEFAULT_GOAL := help

.PHONY: help build build-server build-desktop check test fmt docker docker-run clean

help: ## Mostra os alvos disponíveis
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

build: build-server build-desktop ## Compila gateway e interface

build-server: ## Compila o gateway Go para o host atual
	cd apps/server && $(GO) build -trimpath -buildvcs=false -ldflags="-s -w" -o ../../keyhybrid-server .

build-desktop: ## Instala dependências e compila a interface Svelte
	cd apps/desktop && $(PNPM) install --frozen-lockfile && $(PNPM) run build

check: ## Roda go vet, svelte-check e cargo fmt --check
	cd apps/server && $(GO) vet ./...
	cd apps/desktop && $(PNPM) run check
	cd apps/desktop/src-tauri && cargo fmt --all -- --check

test: ## Roda os testes do gateway e da interface
	cd apps/server && $(GO) test -race ./...
	cd apps/desktop && $(PNPM) run test

fmt: ## Formata o código Go e Rust
	cd apps/server && gofmt -w .
	cd apps/desktop/src-tauri && cargo fmt --all

docker: ## Constrói a imagem do gateway localmente
	docker build -t keyhybrid-gateway:local .

docker-run: ## Roda o gateway em container em 127.0.0.1:8090
	docker compose up -d --build

clean: ## Remove artefatos gerados
	rm -rf apps/server/dist keyhybrid-server keyhybrid-server.exe

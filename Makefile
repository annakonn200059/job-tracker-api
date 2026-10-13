-include .env
export

.PHONY: migrate-new migrate-up migrate-down migrate-status build run dev

SHA   := $(shell git rev-parse --short HEAD)
DIRTY := $(shell git diff --quiet && git diff --cached --quiet || echo "-dirty")
AIR := $(shell go env GOPATH)/bin/air
TAG   := $(SHA)$(DIRTY)

migrate-new:
	goose -dir migrations create $(name) sql

migrate-up:
	goose -dir migrations postgres "$(DATABASE_URL)" up

migrate-down:
	goose -dir migrations postgres "$(DATABASE_URL)" down

migrate-status:
	goose -dir migrations postgres "$(DATABASE_URL)" status

build:
	go build -o bin/api ./cmd/api

run: build
	./bin/api

dev:
	$(AIR)

tag:
	@echo $(TAG)

image:
	docker build -t job-tracker-api:$(TAG) .

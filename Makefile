-include .env
export

.PHONY: migrate-new migrate-up migrate-down migrate-status

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
.PHONY: build test frontend docker
build:
	go build ./backend/cmd/controller
	go build ./agent/cmd/agent
test:
	go test ./...
frontend:
	cd frontend && npm install && npm run build
docker:
	docker compose build

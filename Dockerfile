# syntax=docker/dockerfile:1.7
FROM node:22-alpine AS frontend
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY frontend/ ./
RUN npm run build

FROM golang:1.25-alpine AS controller-build
WORKDIR /src
RUN apk add --no-cache ca-certificates
COPY go.mod go.sum ./
RUN go mod download
COPY backend/ ./backend/
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/controller ./backend/cmd/controller

FROM alpine:3.23
RUN apk add --no-cache ca-certificates tzdata && addgroup -S rotato && adduser -S -G rotato rotato
WORKDIR /app
COPY --from=controller-build /out/controller /app/controller
COPY --from=frontend /src/frontend/dist /app/frontend
COPY install-agent.sh /app/install-agent.sh
RUN mkdir -p /app/data /app/logs && chown -R rotato:rotato /app
USER rotato
ENV STATIC_DIR=/app/frontend DB_PATH=/app/data/app.db INSTALL_AGENT_SCRIPT=/app/install-agent.sh APP_PORT=8080
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD wget -qO- http://127.0.0.1:8080/healthz || exit 1
ENTRYPOINT ["/app/controller"]

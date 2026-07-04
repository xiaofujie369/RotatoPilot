# syntax=docker/dockerfile:1.7
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
COPY agent/ ./agent/
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/agent ./agent/cmd/agent
FROM alpine:3.23
RUN apk add --no-cache ca-certificates tzdata && addgroup -S rotato && adduser -S -G rotato rotato
COPY --from=build /out/agent /usr/local/bin/rotatopilot-agent
USER rotato
ENTRYPOINT ["rotatopilot-agent"]

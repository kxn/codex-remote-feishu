FROM golang:1.24-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o /out/codex-remote ./cmd/codex-remote

FROM alpine:3.21

RUN adduser -D -h /app app
USER app
WORKDIR /app

COPY --from=build /out/codex-remote /usr/local/bin/codex-remote

ENV RELAY_HOST=0.0.0.0 \
    RELAY_PORT=9500 \
    RELAY_API_HOST=0.0.0.0 \
    RELAY_API_PORT=9501 \
    FEISHU_USE_SYSTEM_PROXY=false

EXPOSE 9500 9501

ENTRYPOINT ["/usr/local/bin/codex-remote", "daemon"]

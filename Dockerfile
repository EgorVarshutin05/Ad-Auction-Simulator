
# Этап 1: сборка Go-приложений.
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Сначала копируем зависимости для кеширования Docker.
COPY go.mod go.sum ./
RUN go mod download

# Затем копируем исходный код.
COPY . .

# Собираем три отдельных приложения.
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath -o /app/bin/server ./cmd/server

RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath -o /app/bin/consumer ./cmd/consumer

RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath -o /app/bin/outbox-publisher ./cmd/outbox-publisher


# Этап 2: компактный образ для запуска.
FROM alpine:3.22

WORKDIR /app

# Сертификаты для HTTPS и часовые пояса.
RUN apk add --no-cache ca-certificates tzdata

# Забираем готовые программы из builder.
COPY --from=builder /app/bin/ /app/bin/

# Оставляем веб-файлы на случай,
# если сервер обращается к ним с диска.
COPY --from=builder /app/web/ /app/web/

EXPOSE 8080

# По умолчанию запускаем HTTP-сервер.
CMD ["/app/bin/server"]

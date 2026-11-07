
FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath -o /app/bin/server ./cmd/server

RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath -o /app/bin/consumer ./cmd/consumer

RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath -o /app/bin/outbox-publisher ./cmd/outbox-publisher

RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath -o /app/bin/cache-sync ./cmd/cache-sync

FROM alpine:3.22

WORKDIR /app

RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /app/bin/ /app/bin/
COPY --from=builder /app/web/ /app/web/

EXPOSE 8080

CMD ["/app/bin/server"]

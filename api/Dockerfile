FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /api main.go
RUN CGO_ENABLED=0 go build -o /backfill ./cmd/backfill
RUN CGO_ENABLED=0 go build -o /migrate-session-ledger ./cmd/migrate-session-ledger

FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata
COPY --from=builder /api /usr/local/bin/api
COPY --from=builder /backfill /usr/local/bin/backfill
COPY --from=builder /migrate-session-ledger /usr/local/bin/migrate-session-ledger
EXPOSE 8080
CMD ["api"]

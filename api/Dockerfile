FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /api main.go
RUN CGO_ENABLED=0 go build -o /backfill ./cmd/backfill
RUN CGO_ENABLED=0 go build -o /migrate-session-ledger ./cmd/migrate-session-ledger
RUN CGO_ENABLED=0 go build -o /report-source-catalog-backfill ./cmd/report-source-catalog-backfill
RUN CGO_ENABLED=0 go build -o /token-contribution-backfill ./cmd/token-contribution-backfill
RUN CGO_ENABLED=0 go build -o /audit-session-content ./cmd/audit-session-content
RUN CGO_ENABLED=0 go build -o /compact-session-events ./cmd/compact-session-events

FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata
COPY --from=builder /api /usr/local/bin/api
COPY --from=builder /backfill /usr/local/bin/backfill
COPY --from=builder /migrate-session-ledger /usr/local/bin/migrate-session-ledger
COPY --from=builder /report-source-catalog-backfill /usr/local/bin/report-source-catalog-backfill
COPY --from=builder /token-contribution-backfill /usr/local/bin/token-contribution-backfill
COPY --from=builder /audit-session-content /usr/local/bin/audit-session-content
COPY --from=builder /compact-session-events /usr/local/bin/compact-session-events
EXPOSE 8080
CMD ["api"]

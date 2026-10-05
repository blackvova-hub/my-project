FROM golang:1.26-alpine AS build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
COPY engine ./engine
COPY marketdata ./marketdata
COPY jobs ./jobs
COPY historysync ./historysync
COPY archivesync ./archivesync
COPY cmd ./cmd
RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/backtest ./cmd/worker
RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/history-sync ./cmd/history-sync
RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/archive-sync ./cmd/archive-sync

FROM alpine:3.24
RUN apk add --no-cache ca-certificates && addgroup -S app && adduser -S app -G app
COPY --from=build /bin/backtest /app/backtest
COPY --from=build /bin/history-sync /app/history-sync
COPY --from=build /bin/archive-sync /app/archive-sync
USER app
CMD ["/app/backtest"]

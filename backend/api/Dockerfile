FROM golang:1.26-alpine AS build
WORKDIR /app

COPY go.mod go.sum ./
COPY --from=backtest go.mod go.sum /backtest/
COPY --from=backtest *.go /backtest/
COPY --from=backtest jobs /backtest/jobs
COPY --from=backtest marketdata /backtest/marketdata
COPY --from=backtest engine /backtest/engine
RUN go mod download


COPY cmd ./cmd

COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/app ./cmd/api

FROM alpine:3.24
WORKDIR /app
RUN apk add --no-cache ca-certificates \
	&& addgroup -S app \
	&& adduser -S app -G app
COPY --from=build /bin/app /app/app
RUN chown -R app:app /app
USER app
EXPOSE 8080
CMD ["/app/app"]

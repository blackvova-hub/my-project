FROM golang:1.26-alpine AS build
WORKDIR /src/similarity
COPY similarity/go.mod similarity/go.sum ./
COPY backtest/go.mod backtest/go.sum /src/backtest/
RUN go mod download
COPY backtest /src/backtest
COPY similarity /src/similarity
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/similarity ./cmd/worker
FROM alpine:3.24
RUN apk add --no-cache ca-certificates && addgroup -S app && adduser -S app -G app
COPY --from=build /app/similarity /app/similarity
USER app
CMD ["/app/similarity"]

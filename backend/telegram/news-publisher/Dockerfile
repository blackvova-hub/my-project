FROM golang:1.26-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/telegram-news-publisher ./cmd/news-publisher

FROM alpine:3.24
RUN apk add --no-cache ca-certificates tzdata && adduser -D -H -u 10001 app
USER app
COPY --from=build /out/telegram-news-publisher /usr/local/bin/telegram-news-publisher
ENTRYPOINT ["/usr/local/bin/telegram-news-publisher"]

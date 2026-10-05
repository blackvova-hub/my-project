FROM golang:1.26-alpine AS build

WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/telegram-admin-bot ./cmd/admin-bot

FROM alpine:3.24
RUN apk add --no-cache ca-certificates tzdata
COPY --from=build /out/telegram-admin-bot /usr/local/bin/telegram-admin-bot
ENTRYPOINT ["/usr/local/bin/telegram-admin-bot"]

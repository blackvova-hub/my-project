FROM golang:1.26-alpine AS base
WORKDIR /src
ENV GOWORK=off
COPY go.mod go.sum ./
RUN go mod download
FROM base AS test-deps
RUN apk add --no-cache gcc musl-dev
FROM base AS build
ENV CGO_ENABLED=0
COPY . .
RUN go build -trimpath -o /collector ./cmd/collector && go build -trimpath -o /engine ./cmd/engine
FROM test-deps AS test
ENV CGO_ENABLED=1
COPY . .
CMD ["go", "test", "-race", "./...", "-count=1"]
FROM alpine:3.24
RUN apk add --no-cache ca-certificates && adduser -D -u 10001 arb
COPY --from=build /collector /engine /usr/local/bin/
USER arb
CMD ["engine"]

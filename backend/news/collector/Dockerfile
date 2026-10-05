FROM golang:1.26-alpine AS build
WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/app ./cmd/collector

FROM alpine:3.24
WORKDIR /app
RUN addgroup -S app \
	&& adduser -S app -G app
COPY --from=build /bin/app /app/app
RUN chown -R app:app /app
USER app
CMD ["/app/app"]

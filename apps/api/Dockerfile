FROM golang:1.25.1-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/mail-tracker ./cmd/server

FROM alpine:3.22
RUN apk add --no-cache su-exec && addgroup -S app && adduser -S -G app app
WORKDIR /app
COPY --from=build /out/mail-tracker /app/mail-tracker
COPY migrations /app/migrations
COPY docker-entrypoint.sh /app/docker-entrypoint.sh
RUN mkdir /app/data && chown app:app /app/data
EXPOSE 8080
CMD ["/app/docker-entrypoint.sh"]

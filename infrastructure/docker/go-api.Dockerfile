FROM golang:1.23-alpine AS build

WORKDIR /src

COPY go/go.mod go/go.sum ./
RUN go mod download

COPY go/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/pipeforge-api ./cmd/api
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/pipeforge-migrate ./cmd/migrate

FROM alpine:3.20

RUN addgroup -S pipeforge && adduser -S -G pipeforge pipeforge \
    && apk add --no-cache ca-certificates

COPY --from=build /out/pipeforge-api /usr/local/bin/pipeforge-api
COPY --from=build /out/pipeforge-migrate /usr/local/bin/pipeforge-migrate

USER pipeforge
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/pipeforge-api"]


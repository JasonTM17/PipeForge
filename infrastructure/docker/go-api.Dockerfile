FROM golang:1.25-alpine AS build

WORKDIR /src

COPY go/go.mod go/go.sum ./
RUN go mod download

COPY go/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/pipeforge-api ./cmd/api
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/pipeforge-migrate ./cmd/migrate
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/pipeforge-scheduler ./cmd/scheduler
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/pipeforge-result-consumer ./cmd/result-consumer

FROM alpine:3.20 AS runtime

RUN addgroup -S pipeforge && adduser -S -G pipeforge pipeforge \
    && apk add --no-cache ca-certificates

USER pipeforge

FROM runtime AS api
COPY --from=build /out/pipeforge-api /usr/local/bin/pipeforge-api
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/pipeforge-api"]

FROM runtime AS scheduler
COPY --from=build /out/pipeforge-scheduler /usr/local/bin/pipeforge-scheduler
ENTRYPOINT ["/usr/local/bin/pipeforge-scheduler"]

FROM runtime AS result-consumer
COPY --from=build /out/pipeforge-result-consumer /usr/local/bin/pipeforge-result-consumer
ENTRYPOINT ["/usr/local/bin/pipeforge-result-consumer"]

FROM runtime AS migrate
COPY --from=build /out/pipeforge-migrate /usr/local/bin/pipeforge-migrate
ENTRYPOINT ["/usr/local/bin/pipeforge-migrate"]

FROM golang:1.26.5-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/openfreaks ./cmd/openfreaks

FROM alpine:3.23 AS runtime
RUN addgroup -g 10001 freaks && adduser -D -u 10001 -G freaks freaks \
    && mkdir -p /app/data && chown 10001:10001 /app/data
WORKDIR /app
COPY --from=build /out/openfreaks /app/openfreaks
COPY deploy/config.docker.json /app/deploy/config.docker.json
COPY deploy/content-real /app/content
USER 10001:10001
STOPSIGNAL SIGTERM

FROM runtime AS main
EXPOSE 9609/tcp 9729/tcp
ENTRYPOINT ["/app/openfreaks", "--mode=main", "--config=deploy/config.docker.json"]

FROM runtime AS battle
EXPOSE 9610/udp 9730/tcp
ENTRYPOINT ["/app/openfreaks", "--mode=battle", "--config=deploy/config.docker.json"]

FROM runtime AS all
EXPOSE 9609/tcp 9610/udp 9729/tcp 9730/tcp
ENTRYPOINT ["/app/openfreaks", "--mode=all", "--config=deploy/config.docker.json"]

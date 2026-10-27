FROM golang:1.27.3-alpine AS build
WORKDIR /src
ENV GOWORK=off CGO_ENABLED=0
COPY modules/commerce/go.mod modules/commerce/go.sum modules/commerce/
COPY modules/platform/go.mod modules/platform/go.mod
COPY modules/identity/go.mod modules/identity/go.sum modules/identity/
COPY apps/api/go.mod apps/api/go.sum apps/api/
WORKDIR /src/apps/api
RUN go mod download
WORKDIR /src
COPY modules modules
COPY apps apps
WORKDIR /src/apps/api
RUN go build -trimpath -ldflags='-s -w' -o /out/api ./cmd/api

FROM alpine:3.24
RUN apk add --no-cache ca-certificates && addgroup -g 10001 app && adduser -D -u 10001 -G app app && mkdir /data && chown app:app /data
COPY --from=build /out/api /usr/local/bin/api
USER 10001:10001
ENV APP_ENV=demo HTTP_ADDR=:8080 DB_PATH=/data/ecommerce.db
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 CMD wget -q -O /dev/null http://127.0.0.1:8080/readyz || exit 1
ENTRYPOINT ["/usr/local/bin/api"]

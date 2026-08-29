FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/docsli ./cmd/docsli

FROM alpine:3.24
RUN apk add --no-cache git openssh-client ca-certificates \
    && adduser -D -u 1000 docsli
# Bind-mounted volumes are usually owned by the host user, not uid 1000;
# without this git refuses to touch the repo ("dubious ownership").
ENV GIT_CONFIG_COUNT=1 \
    GIT_CONFIG_KEY_0=safe.directory \
    GIT_CONFIG_VALUE_0=*
COPY --from=build /out/docsli /usr/local/bin/docsli
USER docsli
EXPOSE 8080
ENTRYPOINT ["docsli"]
CMD ["-config", "/config.yml"]

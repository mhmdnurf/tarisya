FROM node:24-alpine AS console-build
WORKDIR /src/console
RUN corepack enable
COPY console/package.json console/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY console/ .
RUN pnpm build

FROM golang:1.24-alpine AS core-build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=console-build /src/console/dist ./internal/webui/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" \
    -o /out/tarisya-core ./cmd/core

FROM alpine:3.22
RUN apk add --no-cache ca-certificates && mkdir -p /data
COPY --from=core-build /out/tarisya-core /usr/local/bin/tarisya-core
EXPOSE 8081
ENTRYPOINT ["tarisya-core"]
FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/tarisya-core ./cmd/core

FROM alpine:3.22
RUN apk add --no-cache ca-certificates
COPY --from=build /out/tarisya-core /usr/local/bin/tarisya-core
EXPOSE 8080
ENTRYPOINT ["tarisya-core"]

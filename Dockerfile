# Сборка
FROM golang:1-alpine AS builder

ARG GIT_SHA
ARG BUILD_TIME
ARG BUILD_RUN_ID

WORKDIR /build

RUN apk add --no-cache git

# Кешируем зависимости
COPY go.mod go.sum ./
RUN go mod download

# Копируем код и собираем
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-w -s -X github.com/basili4-1982/api-gateway/internal/proxy.GitSHA=${GIT_SHA} -X github.com/basili4-1982/api-gateway/internal/proxy.BuildTime=${BUILD_TIME} -X github.com/basili4-1982/api-gateway/internal/proxy.BuildRunID=${BUILD_RUN_ID}" -trimpath -o api-gateway ./cmd/

FROM scratch

# Копируем сертификаты и timezone
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Копируем бинарник
COPY --from=builder /build/api-gateway /app/api-gateway

WORKDIR /app

CMD ["/app/api-gateway"]

# Сборка
FROM golang:1.25-alpine AS builder

ARG TOKEN=token
ARG USER=user

WORKDIR /build

RUN apk add --no-cache git

RUN echo "machine github.com" > ~/.netrc &&  \
    echo "login ${USER}" >> ~/.netrc && \
    echo "password ${TOKEN}" >> ~/.netrc

# Кешируем зависимости
COPY go.mod go.sum ./
RUN go mod download

RUN  mkdir -p /etc/proxy

# Копируем код и собираем
COPY . .
COPY config.yaml /etc/proxy/
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-w -s" -trimpath -o api-gateway ./cmd/

FROM scratch

# Копируем сертификаты и timezone
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
# Копирую конфиг по умолчанию
COPY --from=builder /etc/proxy /etc/proxy

# Копируем бинарник
COPY --from=builder /build/api-gateway /app/api-gateway

# Чистим credentials из builder слоя (безопасность)
RUN rm -f ~/.netrc && git config --global --unset url.insteadOf 2>/dev/null || true

WORKDIR /app

CMD ["/app/api-gateway"]

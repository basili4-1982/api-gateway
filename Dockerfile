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

# Копируем код и собираем
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-w -s" -trimpath -o api-gateway ./cmd/

FROM scratch

# Копируем сертификаты и timezone
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Копируем бинарник
COPY --from=builder /build/api-gateway /app/api-gateway

WORKDIR /app

CMD ["/app/api-gateway"]

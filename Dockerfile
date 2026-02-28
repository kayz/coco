FROM golang:1.24-alpine AS builder
WORKDIR /src

RUN apk add --no-cache git ca-certificates

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/coco .

FROM alpine:3.20
WORKDIR /app

RUN apk add --no-cache ca-certificates wget && adduser -D -u 10001 coco

COPY --from=builder /out/coco /usr/local/bin/coco

EXPOSE 18080 8080 18789

HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
  CMD wget -q -O - http://127.0.0.1:18080/api/status >/dev/null || exit 1

USER coco

ENTRYPOINT ["coco"]
CMD ["web", "--port", "18080"]

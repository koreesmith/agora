FROM golang:1.22-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /app

COPY go.mod go.sum* ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o agora ./cmd/server

FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app
COPY --from=builder /app/agora .

RUN mkdir -p /app/media/avatars /app/media/covers /app/media/posts /app/media/instance

EXPOSE 8080

CMD ["./agora"]

FROM golang:1.22-alpine AS builder

RUN apk --no-cache add git

WORKDIR /app
COPY . .
RUN go mod tidy && go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o agora ./cmd/server

FROM alpine:3.19
RUN apk --no-cache add ca-certificates tzdata netcat-openbsd libheif-tools
WORKDIR /app

COPY --from=builder /app/agora .
COPY --from=builder /app/.env.example .env
COPY --from=builder /app/docs ./docs

RUN mkdir -p /data/uploads/avatars /data/uploads/posts /data/uploads/instance

EXPOSE 8080

CMD ["./agora"]

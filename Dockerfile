FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o leaderboard .

FROM alpine:latest
RUN apk add --no-cache wget
WORKDIR /app
COPY --from=builder /app/leaderboard .
EXPOSE 8080
CMD ["./leaderboard"]

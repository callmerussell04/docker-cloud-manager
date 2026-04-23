FROM golang:alpine3.22 AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go build -o sso ./cmd/sso/main.go

FROM alpine:latest

WORKDIR /app

COPY --from=builder /app/sso .

EXPOSE 50051

CMD ["./sso"]
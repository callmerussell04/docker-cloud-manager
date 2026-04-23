FROM golang:alpine3.22 AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go build -o gateway ./cmd/gateway/main.go

FROM alpine:latest

WORKDIR /app

COPY --from=builder /app/gateway .

EXPOSE 8081

CMD ["./gateway"]
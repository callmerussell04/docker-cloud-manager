FROM golang:alpine3.22 AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go build -o telemetry ./cmd/telemetry/main.go

FROM alpine:latest

WORKDIR /app

COPY --from=builder /app/telemetry .

EXPOSE 8084

CMD ["./telemetry"]

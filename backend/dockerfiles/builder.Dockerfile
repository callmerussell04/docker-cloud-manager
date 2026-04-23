FROM golang:alpine3.22 AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN go build -o core ./cmd/builder/main.go

FROM alpine:latest

WORKDIR /app

COPY --from=builder /app/core .

EXPOSE 8082

CMD ["./core"]
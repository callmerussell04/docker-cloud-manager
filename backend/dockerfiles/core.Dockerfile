FROM golang:1.26.9-alpine3.24@sha256:3082400e369fa24d5fc60bca20edab3f6d604e0c5a690ec66b295eff4dd87ade AS builder

ENV GOTOOLCHAIN=local

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY . .

RUN CGO_ENABLED=0 go build -mod=readonly -trimpath -buildvcs=false -o core ./cmd/core/main.go

FROM alpine:3.24@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6

WORKDIR /app

COPY dockerfiles/runtime-packages.lock /tmp/runtime-packages.lock
RUN xargs apk add --no-cache < /tmp/runtime-packages.lock \
    && rm /tmp/runtime-packages.lock

COPY --from=builder /app/core .

EXPOSE 50052 8083

CMD ["./core"]

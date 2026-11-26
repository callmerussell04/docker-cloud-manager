FROM alpine:3.24@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6
RUN apk add --no-cache socat
USER 65534:65534
# TLS terminates in Core's narrow Registry listener. This process has no signing
# keys, exchange credentials or configurable forwarding destination.
ENTRYPOINT ["socat", "TCP-LISTEN:5000,fork,reuseaddr", "TCP:core-registry:5000"]

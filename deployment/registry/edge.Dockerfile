FROM alpine:3.22
RUN apk add --no-cache socat
USER 65534:65534
# TLS terminates in Core's narrow Registry listener. This process has no signing
# keys, exchange credentials or configurable forwarding destination.
ENTRYPOINT ["socat", "TCP-LISTEN:5000,fork,reuseaddr", "TCP:core-registry:5000"]

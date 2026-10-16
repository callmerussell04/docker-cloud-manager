FROM alpine:3.22
RUN apk add --no-cache iptables
COPY deployment/registry/network-policy.sh /usr/local/bin/network-policy
COPY deployment/registry/network-policy-check.sh /usr/local/bin/network-policy-check
ENTRYPOINT ["sh", "/usr/local/bin/network-policy"]

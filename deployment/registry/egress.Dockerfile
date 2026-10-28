FROM alpine:3.24@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6
COPY deployment/registry/egress-packages.lock /tmp/packages.lock
RUN xargs apk add --no-cache < /tmp/packages.lock \
    && rm /tmp/packages.lock
COPY deployment/registry/squid.conf /etc/squid/squid.conf
USER squid
ENTRYPOINT ["squid", "-N", "-f", "/etc/squid/squid.conf"]

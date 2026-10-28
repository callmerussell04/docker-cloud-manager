FROM alpine:3.24@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6
COPY deployment/registry/network-policy-packages.lock /tmp/packages.lock
RUN xargs apk add --no-cache < /tmp/packages.lock \
    && rm /tmp/packages.lock
COPY deployment/registry/network-policy.sh /usr/local/bin/network-policy
COPY deployment/registry/network-policy-check.sh /usr/local/bin/network-policy-check
ENTRYPOINT ["sh", "/usr/local/bin/network-policy"]

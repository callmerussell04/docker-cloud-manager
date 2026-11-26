#!/bin/sh
set -eu
build_bridge=${BUILD_BRIDGE:-br-dcm-build}
egress_bridge=${EGRESS_BRIDGE:-br-dcm-egress}
proxy_bridge=${PROXY_BRIDGE:-br-dcm-proxy}
build_chain=${BUILD_CHAIN:-DCM-BUILD}
egress_chain=${EGRESS_CHAIN:-DCM-EGRESS}
proxy_chain=${PROXY_CHAIN:-DCM-PROXY}
traefik_ip=${TRAEFIK_PROXY_IP:-172.30.240.2}
test -f /run/policy-ready
# A stale proxy_net with another bridge name would leave the chain unused.
test -d /sys/class/net/"$proxy_bridge"
for tool in iptables ip6tables; do
 forward=FORWARD
 [ "$tool" != iptables ] || forward=DOCKER-USER
 "$tool" -w -C "$forward" -i "$build_bridge" -j "$build_chain"
 "$tool" -w -C "$forward" -i "$egress_bridge" -j "$egress_chain"
 "$tool" -w -C "$forward" -i "$proxy_bridge" -o "$proxy_bridge" -j "$proxy_chain"
 "$tool" -w -C INPUT -i "$build_bridge" -j DROP
 "$tool" -w -C INPUT -i "$egress_bridge" -j DROP
 "$tool" -w -C "$build_chain" -j DROP
 "$tool" -w -C "$egress_chain" -j DROP
 "$tool" -w -C "$proxy_chain" -j DROP
done
iptables -w -C "$proxy_chain" -s "$traefik_ip" -j ACCEPT
iptables -w -C "$proxy_chain" -d "$traefik_ip" -j ACCEPT
test "$(cat /proc/sys/net/bridge/bridge-nf-call-iptables)" = 1
test "$(cat /proc/sys/net/bridge/bridge-nf-call-ip6tables)" = 1

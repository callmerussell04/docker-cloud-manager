#!/bin/sh
set -eu
test -f /run/policy-ready
for tool in iptables ip6tables; do
 forward=FORWARD
 [ "$tool" != iptables ] || forward=DOCKER-USER
 "$tool" -w -C "$forward" -i br-dcm-build -j DCM-BUILD
 "$tool" -w -C "$forward" -i br-dcm-egress -j DCM-EGRESS
 "$tool" -w -C INPUT -i br-dcm-build -j DROP
 "$tool" -w -C INPUT -i br-dcm-egress -j DROP
 "$tool" -w -C DCM-BUILD -j DROP
 "$tool" -w -C DCM-EGRESS -j DROP
done
test "$(cat /proc/sys/net/bridge/bridge-nf-call-iptables)" = 1
test "$(cat /proc/sys/net/bridge/bridge-nf-call-ip6tables)" = 1

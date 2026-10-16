#!/bin/sh
set -eu
# These rules intentionally remain on shutdown. Docker's forwarding chain and
# host INPUT both need protection; an internal bridge alone leaves host access.
build_bridge=${BUILD_BRIDGE:-br-dcm-build}
egress_bridge=${EGRESS_BRIDGE:-br-dcm-egress}
build_chain=${BUILD_CHAIN:-DCM-BUILD}
egress_chain=${EGRESS_CHAIN:-DCM-EGRESS}
edge_ip=${REGISTRY_EDGE_IP:-172.30.252.2}
proxy_ip=${BUILD_PROXY_IP:-172.30.252.3}
for tool in iptables ip6tables; do
 rules=/run/"$tool".rules
 {
  echo '*filter'
  echo ":$build_chain - [0:0]"
  echo ":$egress_chain - [0:0]"
  echo "-F $build_chain"
  echo "-F $egress_chain"
  if [ "$tool" = iptables ]; then
   echo "-A $build_chain -s $edge_ip -p tcp --sport 5000 -m conntrack --ctstate ESTABLISHED -j ACCEPT"
   echo "-A $build_chain -s $proxy_ip -p tcp --sport 3128 -m conntrack --ctstate ESTABLISHED -j ACCEPT"
   echo "-A $build_chain -d $edge_ip -p tcp --dport 5000 -j ACCEPT"
   echo "-A $build_chain -d $proxy_ip -p tcp --dport 3128 -j ACCEPT"
   for range in 0.0.0.0/8 10.0.0.0/8 100.64.0.0/10 127.0.0.0/8 169.254.0.0/16 172.16.0.0/12 192.168.0.0/16 224.0.0.0/4 240.0.0.0/4; do
    echo "-A $egress_chain -d $range -j DROP"
   done
   echo "-A $egress_chain -m addrtype --dst-type LOCAL -j DROP"
   echo "-A $egress_chain -p tcp -m multiport --dports 80,443 -j ACCEPT"
  fi
  echo "-A $build_chain -j DROP"
  echo "-A $egress_chain -j DROP"
  echo COMMIT
 } > "$rules"
 "$tool"-restore -w --noflush < "$rules"
 # IPv6 may have no Docker chain when disabled. Still protect FORWARD and INPUT.
 for bridge in "$build_bridge" "$egress_bridge"; do
  chain=$build_chain
  [ "$bridge" != "$egress_bridge" ] || chain=$egress_chain
  forward=FORWARD
  if [ "$tool" = iptables ]; then forward=DOCKER-USER; fi
  "$tool" -w -C "$forward" -i "$bridge" -j "$chain" 2>/dev/null || "$tool" -w -I "$forward" 1 -i "$bridge" -j "$chain"
  "$tool" -w -C INPUT -i "$bridge" -j DROP 2>/dev/null || "$tool" -w -I INPUT 1 -i "$bridge" -j DROP
 done
 done
# Ensure same-bridge traffic passes the host filter as well.
test "$(cat /proc/sys/net/bridge/bridge-nf-call-iptables)" = 1
test "$(cat /proc/sys/net/bridge/bridge-nf-call-ip6tables)" = 1
touch /run/policy-ready
exec tail -f /dev/null

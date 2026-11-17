#!/bin/sh
set -eu
# These rules intentionally remain on shutdown. Docker's forwarding chain and
# host INPUT both need protection; an internal bridge alone leaves host access.
# The proxy bridge chain sees only same-bridge traffic: exposed containers reach
# each other only to or from Traefik, and their other traffic is unchanged.
build_bridge=${BUILD_BRIDGE:-br-dcm-build}
egress_bridge=${EGRESS_BRIDGE:-br-dcm-egress}
proxy_bridge=${PROXY_BRIDGE:-br-dcm-proxy}
build_chain=${BUILD_CHAIN:-DCM-BUILD}
egress_chain=${EGRESS_CHAIN:-DCM-EGRESS}
proxy_chain=${PROXY_CHAIN:-DCM-PROXY}
edge_ip=${REGISTRY_EDGE_IP:-172.30.252.2}
proxy_ip=${BUILD_PROXY_IP:-172.30.252.3}
traefik_ip=${TRAEFIK_PROXY_IP:-172.30.240.2}
for tool in iptables ip6tables; do
 rules=/run/"$tool".rules
 {
  echo '*filter'
  echo ":$build_chain - [0:0]"
  echo ":$egress_chain - [0:0]"
  echo ":$proxy_chain - [0:0]"
  echo "-F $build_chain"
  echo "-F $egress_chain"
  echo "-F $proxy_chain"
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
   echo "-A $proxy_chain -s $traefik_ip -j ACCEPT"
   echo "-A $proxy_chain -d $traefik_ip -j ACCEPT"
  fi
  echo "-A $build_chain -j DROP"
  echo "-A $egress_chain -j DROP"
  echo "-A $proxy_chain -j DROP"
  echo COMMIT
 } > "$rules"
 "$tool"-restore -w --noflush < "$rules"
 # IPv6 may have no Docker chain when disabled. Still protect FORWARD and INPUT.
 forward=FORWARD
 if [ "$tool" = iptables ]; then forward=DOCKER-USER; fi
 for bridge in "$build_bridge" "$egress_bridge"; do
  chain=$build_chain
  [ "$bridge" != "$egress_bridge" ] || chain=$egress_chain
  "$tool" -w -C "$forward" -i "$bridge" -j "$chain" 2>/dev/null || "$tool" -w -I "$forward" 1 -i "$bridge" -j "$chain"
  "$tool" -w -C INPUT -i "$bridge" -j DROP 2>/dev/null || "$tool" -w -I INPUT 1 -i "$bridge" -j DROP
 done
 "$tool" -w -C "$forward" -i "$proxy_bridge" -o "$proxy_bridge" -j "$proxy_chain" 2>/dev/null || "$tool" -w -I "$forward" 1 -i "$proxy_bridge" -o "$proxy_bridge" -j "$proxy_chain"
 done
# Ensure same-bridge traffic passes the host filter as well.
test "$(cat /proc/sys/net/bridge/bridge-nf-call-iptables)" = 1
test "$(cat /proc/sys/net/bridge/bridge-nf-call-ip6tables)" = 1
touch /run/policy-ready
exec tail -f /dev/null

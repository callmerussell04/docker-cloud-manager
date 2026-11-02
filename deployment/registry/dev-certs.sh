#!/bin/sh
# Creates throwaway Registry certificates for a personal machine. Production
# certificates come from your own CA: see docs/deployment-single-vm.md.
set -eu

certs_dir=${1:-/etc/dcm/registry}
docker_certs_dir=${DOCKER_CERTS_DIR:-/etc/docker/certs.d/registry:5000}
hosts_file=${HOSTS_FILE:-/etc/hosts}
files='ca.crt registry.crt registry.key backend.crt backend.key signing.crt signing.key'

mkdir -p "$certs_dir"

# Docker creates a missing bind-mount source as an empty directory. rmdir fails
# on anything with contents, so only those placeholders can be removed here.
present=0
missing=0
for file in $files; do
 path=$certs_dir/$file
 if [ -d "$path" ]; then rmdir "$path"; fi
 if [ -e "$path" ]; then present=$((present + 1)); else missing=$((missing + 1)); fi
done

if [ "$present" -gt 0 ] && [ "$missing" -gt 0 ]; then
 echo "$certs_dir has only some of the certificate files; refusing to overwrite them. Expected: $files" >&2
 exit 1
fi

if [ "$missing" -gt 0 ]; then
 umask 077
 work=$(mktemp -d)
 trap 'rm -rf "$work"' EXIT
 openssl req -x509 -newkey rsa:4096 -nodes -days 3650 -subj '/CN=DCM Dev Registry CA' \
  -keyout "$work/ca.key" -out "$work/ca.crt" 2>/dev/null
 for pair in registry:registry backend:registry-backend; do
  name=${pair%%:*}
  san=${pair##*:}
  openssl req -newkey rsa:2048 -nodes -subj "/CN=$san" \
   -keyout "$work/$name.key" -out "$work/$name.csr" 2>/dev/null
  printf 'subjectAltName=DNS:%s\nextendedKeyUsage=serverAuth\nkeyUsage=digitalSignature,keyEncipherment\n' "$san" > "$work/$name.ext"
  openssl x509 -req -in "$work/$name.csr" -CA "$work/ca.crt" -CAkey "$work/ca.key" -CAcreateserial \
   -days 825 -sha256 -extfile "$work/$name.ext" -out "$work/$name.crt" 2>/dev/null
 done
 # Core signs Registry tokens with RS256, so this must stay an RSA key.
 openssl req -x509 -newkey rsa:2048 -nodes -days 825 -subj '/CN=DCM Dev Registry Token Signing' \
  -keyout "$work/signing.key" -out "$work/signing.crt" 2>/dev/null
 for file in $files; do
  case $file in
   *.key) chmod 600 "$work/$file" ;;
   *) chmod 644 "$work/$file" ;;
  esac
  mv "$work/$file" "$certs_dir/$file"
 done
 chmod 755 "$certs_dir"
 echo "Created Registry certificates in $certs_dir"
else
 echo "Registry certificates already exist in $certs_dir; keeping them"
fi

# Builds and image pulls reach the Registry from the Docker host by this name.
mkdir -p "$docker_certs_dir"
cp "$certs_dir/ca.crt" "$docker_certs_dir/ca.crt"
chmod 644 "$docker_certs_dir/ca.crt"
grep -qE '^[^#]*[[:space:]]registry([[:space:]]|$)' "$hosts_file" || echo '127.0.0.1 registry' >> "$hosts_file"

echo 'Run docker compose down before starting the stack so containers pick up these files.'

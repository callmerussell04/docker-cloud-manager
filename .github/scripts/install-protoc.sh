#!/bin/sh
# Installs the pinned protoc release after verifying its checksum.
# Requires PROTOC_VERSION and PROTOC_SHA256 (linux-x86_64 archive).
set -eu

archive=protoc-$PROTOC_VERSION-linux-x86_64.zip
dir=$RUNNER_TEMP/protoc-$PROTOC_VERSION

curl -sSfL -o "$RUNNER_TEMP/$archive" \
	"https://github.com/protocolbuffers/protobuf/releases/download/v$PROTOC_VERSION/$archive"
printf '%s  %s\n' "$PROTOC_SHA256" "$RUNNER_TEMP/$archive" | sha256sum -c -
unzip -q -o "$RUNNER_TEMP/$archive" -d "$dir"
printf '%s\n' "$dir/bin" >>"$GITHUB_PATH"

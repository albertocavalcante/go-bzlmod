#!/usr/bin/env bash
set -euo pipefail
umask 077

if [[ $# != 1 ]]; then
  echo "Usage: $0 INSTALL_DIRECTORY" >&2
  exit 2
fi

# Digests published alongside the 7.0.0 artifacts at releases.bazel.build.
version=7.0.0
case "$(uname -s)-$(uname -m)" in
  Linux-x86_64)
    platform=linux-x86_64
    sha256=8b24f70542f9736e0ccbf9cb3335cb9f049b5a6787f1e5997ed1da80b9bbd46e
    ;;
  Darwin-arm64)
    platform=darwin-arm64
    sha256=b1cf1c5783fa3eac60942bf2ec6169cfccecd2fa5a355587d8d35983bc1e1310
    ;;
  *)
    echo "Unsupported Bazel test platform" >&2
    exit 1
    ;;
esac

mkdir -p "$1"
staged=$(mktemp "$1/.bazel.XXXXXX")
trap 'rm -f "$staged"' EXIT
curl --fail --silent --show-error --location \
  "https://releases.bazel.build/$version/release/bazel-$version-$platform" -o "$staged"
printf '%s  %s\n' "$sha256" "$staged" | shasum -a 256 -c -
chmod 700 "$staged"
mv "$staged" "$1/bazel"

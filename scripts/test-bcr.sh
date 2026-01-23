#!/bin/bash
# Test go-bzlmod parser against all MODULE.bazel files in Bazel Central Registry
#
# Usage:
#   ./scripts/test-bcr.sh           # Clone BCR (shallow) and test
#   ./scripts/test-bcr.sh --strict  # Require 100% success rate
#   BCR_PATH=/path/to/bcr ./scripts/test-bcr.sh  # Use existing BCR checkout

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
BCR_CACHE_DIR="${BCR_CACHE_DIR:-$PROJECT_DIR/.bcr-cache}"
BCR_REPO="https://github.com/bazelbuild/bazel-central-registry.git"

STRICT_MODE=false
if [[ "$1" == "--strict" ]]; then
  STRICT_MODE=true
fi

# Use existing BCR_PATH if set, otherwise clone/update
if [[ -z "$BCR_PATH" ]]; then
  echo "BCR_PATH not set, using cache at $BCR_CACHE_DIR"

  if [[ -d "$BCR_CACHE_DIR/.git" ]]; then
    echo "Updating existing BCR clone..."
    cd "$BCR_CACHE_DIR"
    git fetch --depth=1 origin main
    git reset --hard origin/main
  else
    echo "Cloning BCR (shallow clone, modules directory only)..."
    mkdir -p "$BCR_CACHE_DIR"
    cd "$BCR_CACHE_DIR"
    git init
    git remote add origin "$BCR_REPO"
    git config core.sparseCheckout true
    echo "modules/" >.git/info/sparse-checkout
    git fetch --depth=1 origin main
    git checkout main
  fi

  BCR_PATH="$BCR_CACHE_DIR"
fi

# Count MODULE.bazel files
MODULE_COUNT=$(find "$BCR_PATH/modules" -name "MODULE.bazel" | wc -l | tr -d ' ')
echo ""
echo "Found $MODULE_COUNT MODULE.bazel files in BCR"
echo ""

# Run the test
cd "$PROJECT_DIR"
export BCR_PATH

if $STRICT_MODE; then
  echo "Running strict test (100% success required)..."
  go test -v -run TestParseBCRStrict ./ast/...
else
  echo "Running standard test (95% success threshold)..."
  go test -v -run TestParseBCR ./ast/... -timeout 5m
fi

echo ""
echo "Done!"

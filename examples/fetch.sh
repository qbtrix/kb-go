#!/bin/bash
# fetch.sh — Download medium and large test corpora for benchmarking.
# Usage: ./examples/fetch.sh [medium|large|all]
# Pinned to specific tags for reproducible benchmarks.

set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
MEDIUM_DIR="$SCRIPT_DIR/medium"
LARGE_DIR="$SCRIPT_DIR/large"

# Pinned versions for reproducibility
LITESTREAM_TAG="v0.5.10"
LITESTREAM_REPO="https://github.com/benbjohnson/litestream.git"

FLASK_TAG="3.1.0"
FLASK_REPO="https://github.com/pallets/flask.git"

fetch_medium() {
    if [ -d "$MEDIUM_DIR/litestream" ]; then
        echo "Medium corpus already exists: $MEDIUM_DIR/litestream"
        return
    fi
    echo "Fetching medium corpus: litestream@$LITESTREAM_TAG..."
    git clone --depth 1 --branch "$LITESTREAM_TAG" "$LITESTREAM_REPO" "$MEDIUM_DIR/litestream" 2>&1
    # Count files
    GO_COUNT=$(find "$MEDIUM_DIR/litestream" -name "*.go" | wc -l | tr -d ' ')
    echo "Downloaded: $GO_COUNT .go files"
}

fetch_large() {
    if [ -d "$LARGE_DIR/flask" ]; then
        echo "Large corpus already exists: $LARGE_DIR/flask"
        return
    fi
    echo "Fetching large corpus: flask@$FLASK_TAG..."
    git clone --depth 1 --branch "$FLASK_TAG" "$FLASK_REPO" "$LARGE_DIR/flask" 2>&1
    # Count files
    PY_COUNT=$(find "$LARGE_DIR/flask" -name "*.py" | wc -l | tr -d ' ')
    echo "Downloaded: $PY_COUNT .py files"
}

TARGET="${1:-all}"

case "$TARGET" in
    medium)
        fetch_medium
        ;;
    large)
        fetch_large
        ;;
    all)
        fetch_medium
        fetch_large
        ;;
    clean)
        echo "Cleaning downloaded corpora..."
        rm -rf "$MEDIUM_DIR/litestream" "$LARGE_DIR/flask"
        echo "Done."
        ;;
    *)
        echo "Usage: $0 [medium|large|all|clean]"
        exit 1
        ;;
esac

echo ""
echo "Corpus status:"
[ -d "$MEDIUM_DIR/litestream" ] && echo "  medium: litestream@$LITESTREAM_TAG ✓" || echo "  medium: not downloaded"
[ -d "$LARGE_DIR/flask" ] && echo "  large:  flask@$FLASK_TAG ✓" || echo "  large:  not downloaded"

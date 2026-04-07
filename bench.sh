#!/bin/bash
# bench.sh — Full pipeline benchmarks for kb-go.
# Measures: cold build, warm build (cache), search latency + relevance, quality.
# Usage: ./bench.sh [small|medium|large|all]
# Requires: ./kb binary built, ANTHROPIC_API_KEY for build/lint benchmarks.

set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
KB="$SCRIPT_DIR/kb"
EXAMPLES="$SCRIPT_DIR/examples"
GOLDEN="$EXAMPLES/golden"
RESULTS_FILE="$SCRIPT_DIR/bench_results.json"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

# --- Checks ---

if [ ! -f "$KB" ]; then
    echo -e "${RED}Error: kb binary not found. Run: go build -o kb .${NC}"
    exit 1
fi

HAS_API_KEY=false
if [ -n "$ANTHROPIC_API_KEY" ]; then
    HAS_API_KEY=true
fi

# --- Timer ---

timer_start() { TIMER_START=$(date +%s%N); }
timer_stop() {
    local end=$(date +%s%N)
    TIMER_MS=$(( (end - TIMER_START) / 1000000 ))
    TIMER_SEC=$(echo "scale=2; $TIMER_MS / 1000" | bc)
}

# --- Benchmark Functions ---

bench_build_cold() {
    local path="$1" scope="$2" pattern="$3"
    echo -e "${CYAN}  Cold build: $scope${NC}"

    # Clear any existing KB
    "$KB" clear --scope "$scope" 2>/dev/null || true

    timer_start
    local output=$("$KB" build "$path" --scope "$scope" --pattern "$pattern" --json 2>&1)
    timer_stop

    local changed=$(echo "$output" | python3 -c "import sys,json; print(json.load(sys.stdin).get('changed',0))" 2>/dev/null || echo "?")
    local total=$(echo "$output" | python3 -c "import sys,json; print(json.load(sys.stdin).get('total',0))" 2>/dev/null || echo "?")

    echo "    Files: $changed compiled, $total total"
    echo "    Time: ${TIMER_SEC}s"

    BUILD_COLD_SEC="$TIMER_SEC"
    BUILD_COLD_FILES="$total"
    BUILD_COLD_CHANGED="$changed"

    if [ "$total" != "?" ] && [ "$total" != "0" ]; then
        local per_file=$(echo "scale=2; $TIMER_MS / $changed / 1000" | bc 2>/dev/null || echo "?")
        echo "    Per file: ${per_file}s"
        BUILD_COLD_PER_FILE="$per_file"
    fi
}

bench_build_warm() {
    local path="$1" scope="$2" pattern="$3"
    echo -e "${CYAN}  Warm build (cache): $scope${NC}"

    timer_start
    local output=$("$KB" build "$path" --scope "$scope" --pattern "$pattern" --json 2>&1)
    timer_stop

    local cached=$(echo "$output" | python3 -c "import sys,json; print(json.load(sys.stdin).get('cached',0))" 2>/dev/null || echo "?")
    local total=$(echo "$output" | python3 -c "import sys,json; print(json.load(sys.stdin).get('total',0))" 2>/dev/null || echo "?")

    local hit_pct="?"
    if [ "$total" != "?" ] && [ "$total" != "0" ]; then
        hit_pct=$(echo "scale=1; $cached * 100 / $total" | bc 2>/dev/null || echo "?")
    fi

    echo "    Cached: $cached / $total ($hit_pct%)"
    echo "    Time: ${TIMER_SEC}s"

    BUILD_WARM_SEC="$TIMER_SEC"
    BUILD_WARM_HIT_PCT="$hit_pct"
}

bench_search() {
    local scope="$1"
    echo -e "${CYAN}  Search benchmarks: $scope${NC}"

    if [ ! -f "$GOLDEN/search_relevance.json" ]; then
        echo "    Skipped: golden/search_relevance.json not found"
        SEARCH_AVG_MS="?"
        SEARCH_RELEVANCE="?"
        return
    fi

    local total_ms=0
    local hits=0
    local tested=0

    # Extract cases for this scope
    local cases=$(python3 -c "
import json
with open('$GOLDEN/search_relevance.json') as f:
    data = json.load(f)
for c in data['cases']:
    if c['scope'] == '$scope':
        print(c['query'] + '|' + c['expected_top_contains'])
" 2>/dev/null)

    if [ -z "$cases" ]; then
        echo "    No test cases for scope: $scope"
        SEARCH_AVG_MS="0"
        SEARCH_RELEVANCE="0"
        return
    fi

    while IFS='|' read -r query expected; do
        timer_start
        local result=$("$KB" search "$query" --scope "$scope" --limit 1 --json 2>/dev/null)
        timer_stop
        total_ms=$((total_ms + TIMER_MS))
        tested=$((tested + 1))

        # Check relevance
        local top_id=$(echo "$result" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d[0]['id'] if d else '')" 2>/dev/null || echo "")
        if echo "$top_id" | grep -qi "$expected"; then
            hits=$((hits + 1))
            echo "    ✓ \"$query\" → $top_id (${TIMER_MS}ms)"
        else
            echo "    ✗ \"$query\" → $top_id (expected *$expected*) (${TIMER_MS}ms)"
        fi
    done <<< "$cases"

    if [ "$tested" -gt 0 ]; then
        SEARCH_AVG_MS=$(echo "scale=1; $total_ms / $tested" | bc)
        SEARCH_RELEVANCE=$(echo "scale=2; $hits / $tested" | bc)
        echo "    Avg latency: ${SEARCH_AVG_MS}ms | Relevance: ${SEARCH_RELEVANCE} ($hits/$tested)"
    fi
}

bench_quality() {
    local scope="$1"
    echo -e "${CYAN}  Quality metrics: $scope${NC}"

    local stats=$("$KB" stats --scope "$scope" --json 2>/dev/null)
    if [ -z "$stats" ]; then
        echo "    No stats available"
        return
    fi

    QUALITY_ARTICLES=$(echo "$stats" | python3 -c "import sys,json; print(json.load(sys.stdin).get('articles',0))" 2>/dev/null)
    QUALITY_CONCEPTS=$(echo "$stats" | python3 -c "import sys,json; print(json.load(sys.stdin).get('concepts',0))" 2>/dev/null)
    QUALITY_WORDS=$(echo "$stats" | python3 -c "import sys,json; print(json.load(sys.stdin).get('words',0))" 2>/dev/null)
    QUALITY_CATEGORIES=$(echo "$stats" | python3 -c "import sys,json; print(json.load(sys.stdin).get('categories',0))" 2>/dev/null)

    local avg_words="?"
    if [ "$QUALITY_ARTICLES" -gt 0 ] 2>/dev/null; then
        avg_words=$((QUALITY_WORDS / QUALITY_ARTICLES))
    fi

    local avg_concepts="?"
    if [ "$QUALITY_ARTICLES" -gt 0 ] 2>/dev/null && [ "$QUALITY_CONCEPTS" -gt 0 ] 2>/dev/null; then
        avg_concepts=$(echo "scale=1; $QUALITY_CONCEPTS / $QUALITY_ARTICLES" | bc 2>/dev/null || echo "?")
    fi

    echo "    Articles: $QUALITY_ARTICLES"
    echo "    Total words: $QUALITY_WORDS (avg: $avg_words/article)"
    echo "    Concepts: $QUALITY_CONCEPTS (avg: $avg_concepts/article)"
    echo "    Categories: $QUALITY_CATEGORIES"

    QUALITY_AVG_WORDS="$avg_words"
    QUALITY_AVG_CONCEPTS="$avg_concepts"
}

# --- Run a Full Benchmark ---

run_bench() {
    local name="$1" path="$2" scope="$3" pattern="$4"

    echo ""
    echo -e "${GREEN}═══════════════════════════════════════${NC}"
    echo -e "${GREEN}  Benchmark: $name${NC}"
    echo -e "${GREEN}═══════════════════════════════════════${NC}"

    # Init result vars
    BUILD_COLD_SEC="?" BUILD_COLD_FILES="?" BUILD_COLD_CHANGED="?" BUILD_COLD_PER_FILE="?"
    BUILD_WARM_SEC="?" BUILD_WARM_HIT_PCT="?"
    SEARCH_AVG_MS="?" SEARCH_RELEVANCE="?"
    QUALITY_ARTICLES="?" QUALITY_CONCEPTS="?" QUALITY_WORDS="?" QUALITY_CATEGORIES="?"
    QUALITY_AVG_WORDS="?" QUALITY_AVG_CONCEPTS="?"

    if [ "$HAS_API_KEY" = true ]; then
        bench_build_cold "$path" "$scope" "$pattern"
        echo ""
        bench_build_warm "$path" "$scope" "$pattern"
    else
        echo -e "${YELLOW}  Skipping build (no ANTHROPIC_API_KEY)${NC}"
    fi

    echo ""
    bench_search "$scope"
    echo ""
    bench_quality "$scope"

    # Write JSON result
    cat >> "$RESULTS_FILE.tmp" << ENTRY
  {
    "corpus": "$name",
    "build_cold": {"wall_sec": $BUILD_COLD_SEC, "files": $BUILD_COLD_FILES, "changed": $BUILD_COLD_CHANGED, "per_file_sec": $BUILD_COLD_PER_FILE},
    "build_warm": {"wall_sec": $BUILD_WARM_SEC, "cache_hit_pct": $BUILD_WARM_HIT_PCT},
    "search": {"avg_latency_ms": $SEARCH_AVG_MS, "relevance_score": $SEARCH_RELEVANCE},
    "quality": {"articles": $QUALITY_ARTICLES, "avg_word_count": $QUALITY_AVG_WORDS, "avg_concepts": $QUALITY_AVG_CONCEPTS, "categories": $QUALITY_CATEGORIES}
  },
ENTRY
}

# --- Main ---

TARGET="${1:-small}"

echo -e "${GREEN}kb-go Benchmark Suite${NC}"
echo "Date: $(date -u +%Y-%m-%dT%H:%M:%SZ)"
echo "API key: $([ "$HAS_API_KEY" = true ] && echo "set" || echo "not set (build benchmarks skipped)")"
echo "Binary: $KB"

# Init results file
echo "[" > "$RESULTS_FILE.tmp"

case "$TARGET" in
    small)
        run_bench "small-go" "$EXAMPLES/small/go" "small-go" "*.go"
        run_bench "small-python" "$EXAMPLES/small/python" "small-python" "*.py"
        run_bench "small-typescript" "$EXAMPLES/small/typescript" "small-typescript" "*.ts"
        ;;
    medium)
        if [ ! -d "$EXAMPLES/medium/litestream" ]; then
            echo -e "${RED}Medium corpus not downloaded. Run: ./examples/fetch.sh medium${NC}"
            exit 1
        fi
        run_bench "medium-litestream" "$EXAMPLES/medium/litestream" "bench-litestream" "*.go"
        ;;
    large)
        if [ ! -d "$EXAMPLES/large/flask" ]; then
            echo -e "${RED}Large corpus not downloaded. Run: ./examples/fetch.sh large${NC}"
            exit 1
        fi
        run_bench "large-flask" "$EXAMPLES/large/flask" "bench-flask" "*.py"
        ;;
    all)
        run_bench "small-go" "$EXAMPLES/small/go" "small-go" "*.go"
        run_bench "small-python" "$EXAMPLES/small/python" "small-python" "*.py"
        run_bench "small-typescript" "$EXAMPLES/small/typescript" "small-typescript" "*.ts"
        [ -d "$EXAMPLES/medium/litestream" ] && run_bench "medium-litestream" "$EXAMPLES/medium/litestream" "bench-litestream" "*.go"
        [ -d "$EXAMPLES/large/flask" ] && run_bench "large-flask" "$EXAMPLES/large/flask/src" "bench-flask" "*.py"
        ;;
    offline)
        echo ""
        echo -e "${CYAN}Running Go benchmarks (offline)...${NC}"
        cd "$SCRIPT_DIR" && go test -bench=. -benchmem -benchtime=1s -count=1 2>&1
        exit 0
        ;;
    *)
        echo "Usage: $0 [small|medium|large|all|offline]"
        exit 1
        ;;
esac

# Finalize results JSON
# Remove trailing comma and close array
sed -i '' '$ s/,$//' "$RESULTS_FILE.tmp" 2>/dev/null || sed -i '$ s/,$//' "$RESULTS_FILE.tmp"
echo "]" >> "$RESULTS_FILE.tmp"
mv "$RESULTS_FILE.tmp" "$RESULTS_FILE"

echo ""
echo -e "${GREEN}═══════════════════════════════════════${NC}"
echo -e "${GREEN}  Results saved to: bench_results.json${NC}"
echo -e "${GREEN}═══════════════════════════════════════${NC}"

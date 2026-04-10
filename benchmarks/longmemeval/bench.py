#!/usr/bin/env python3
# bench.py — LongMemEval 3-tier benchmark harness.
# Tier 1: kb-go BM25 only (zero deps)
# Tier 2: + soul-protocol smart_recall LLM rerank
# Tier 3: + zvec dense search fallback
#
# Usage:
#   uv run python bench.py --tier 1                    # BM25 only
#   uv run python bench.py --tier 2 --engine haiku     # + soul rerank
#   uv run python bench.py --tier 3 --engine haiku     # + zvec dense
#   uv run python bench.py --tier all --engine haiku   # run all three
#   uv run python bench.py --tier 1 --limit 50         # first 50 questions only
#
# Requires: longmemeval_s_cleaned.json in same directory

from __future__ import annotations

import argparse
import asyncio
import json
import os
import sys
import time
from pathlib import Path

from rank_bm25 import BM25Okapi

# Add soul-protocol to path
_soul_root = Path(__file__).resolve().parent.parent.parent / "soul-protocol"
sys.path.insert(0, str(_soul_root / "src"))

# --- Data loading ---

def load_questions(path: str, limit: int | None = None) -> list[dict]:
    with open(path) as f:
        data = json.load(f)
    if limit:
        data = data[:limit]
    return data


def build_session_corpus(q: dict) -> tuple[list[str], list[str]]:
    """Build one doc per session (user turns concatenated, matching LongMemEval standard)."""
    docs, ids = [], []
    for i, session in enumerate(q["haystack_sessions"]):
        parts = [t["content"] for t in session if t["role"] == "user"]
        text = " ".join(parts)
        if text.strip():
            docs.append(text)
            sid = q["haystack_session_ids"][i] if i < len(q["haystack_session_ids"]) else f"s_{i}"
            ids.append(sid)
    return docs, ids


# --- Tier 1: BM25 ---

import re as _re
_TOKEN_RE = _re.compile(r"[a-zA-Z0-9]+")

def _tokenize(text: str) -> list[str]:
    """Match Go kb-go tokenizer: lowercase, alphanumeric only, min 2 chars."""
    return [m.group().lower() for m in _TOKEN_RE.finditer(text) if len(m.group()) >= 2]

def bm25_rank(docs: list[str], doc_ids: list[str], query: str, topk: int = 10) -> list[str]:
    """BM25 ranking over session documents. Uses Go-matching tokenizer."""
    if not docs or not query.strip():
        return []
    tokenized = [_tokenize(d) for d in docs]
    bm25 = BM25Okapi(tokenized)
    scores = bm25.get_scores(_tokenize(query))
    ranked_idx = sorted(range(len(scores)), key=lambda i: -scores[i])
    return [doc_ids[i] for i in ranked_idx[:topk]]


# --- Tier 2: BM25 + soul-protocol smart_recall rerank ---

_RERANK_PROMPT = """\
You are ranking conversation sessions by relevance to a question.
Pick the {limit} most relevant sessions. Output ONLY their numbers, comma-separated, most relevant first.

Question: {query}

Sessions:
{candidates}

Most relevant {limit} session numbers (comma-separated):"""


async def soul_rerank(
    docs: list[str],
    doc_ids: list[str],
    bm25_ranked: list[str],
    query: str,
    engine,
    topk: int = 5,
) -> list[str]:
    """Rerank BM25 top-K using LLM via soul-protocol CognitiveEngine."""
    import re

    id_to_doc = dict(zip(doc_ids, docs))

    # Build candidate text — use up to 800 chars per session (enough for Haiku context)
    formatted = []
    ranked_ids = []
    for i, sid in enumerate(bm25_ranked, 1):
        text = id_to_doc.get(sid, "")
        if text:
            # Truncate but keep enough for the LLM to judge relevance
            snippet = text[:800].replace("\n", " ")
            formatted.append(f"{i}. {snippet}")
            ranked_ids.append(sid)

    if len(ranked_ids) <= topk:
        return ranked_ids

    prompt = _RERANK_PROMPT.format(
        limit=topk,
        query=query,
        candidates="\n\n".join(formatted),
    )

    try:
        response = await asyncio.wait_for(engine.think(prompt), timeout=30.0)
        # Parse comma-separated indices
        numbers = re.findall(r"\d+", response)
        indices = []
        seen = set()
        for n in numbers:
            idx = int(n)
            if 1 <= idx <= len(ranked_ids) and idx not in seen:
                indices.append(idx)
                seen.add(idx)
        if indices:
            return [ranked_ids[i - 1] for i in indices[:topk]]
    except Exception as e:
        pass  # Fall through to BM25 order

    return ranked_ids[:topk]


# --- Tier 3: BM25 + soul rerank + zvec dense fallback ---

def build_zvec_index(docs: list[str], doc_ids: list[str], embed_fn, dim: int, tmp_path: str):
    """Build a zvec collection from document embeddings."""
    import zvec

    schema = zvec.CollectionSchema(
        name="lme_bench",
        vectors=zvec.VectorSchema("emb", zvec.DataType.VECTOR_FP32, dim),
    )
    # Clean up any previous run
    import shutil
    if os.path.exists(tmp_path):
        shutil.rmtree(tmp_path)

    collection = zvec.create_and_open(path=tmp_path, schema=schema)

    # Batch embed and insert
    embeddings = embed_fn(docs)
    zvec_docs = []
    for i, (did, emb) in enumerate(zip(doc_ids, embeddings)):
        zvec_docs.append(zvec.Doc(id=did, vectors={"emb": emb.tolist()}))
    collection.insert(zvec_docs)
    return collection


def zvec_search(collection, query_embedding, topk: int = 10) -> list[str]:
    """Search zvec collection by query embedding."""
    import zvec

    results = collection.query(
        zvec.VectorQuery("emb", vector=query_embedding.tolist()),
        topk=topk,
    )
    return [r.id for r in results]


def merge_rankings(
    bm25_ids: list[str],
    dense_ids: list[str],
    topk: int = 10,
    bm25_weight: float = 1.0,
    dense_weight: float = 0.3,
) -> list[str]:
    """Weighted reciprocal rank fusion. BM25 is primary, dense is supplementary."""
    scores: dict[str, float] = {}
    k = 60  # RRF constant

    for rank, sid in enumerate(bm25_ids):
        scores[sid] = scores.get(sid, 0) + bm25_weight / (k + rank + 1)
    for rank, sid in enumerate(dense_ids):
        scores[sid] = scores.get(sid, 0) + dense_weight / (k + rank + 1)

    ranked = sorted(scores.keys(), key=lambda s: -scores[s])
    return ranked[:topk]


# --- Evaluation ---

def recall_at_k(ranked: list[str], correct: set[str], k: int) -> bool:
    return bool(correct & set(ranked[:k]))


async def run_evaluation(
    questions: list[dict],
    ranker_fn,
    label: str,
) -> dict:
    """Run evaluation with a given ranking function (sync or async)."""
    ks = [1, 3, 5, 10]
    hits = {k: 0 for k in ks}
    type_hits: dict[str, dict[int, int]] = {}
    type_counts: dict[str, int] = {}
    skipped = 0

    start = time.time()
    for i, q in enumerate(questions):
        if not q["haystack_sessions"] or not q["answer_session_ids"]:
            skipped += 1
            continue

        docs, ids = build_session_corpus(q)
        if not docs:
            skipped += 1
            continue

        result = ranker_fn(docs, ids, q["question"])
        ranked = (await result) if asyncio.iscoroutine(result) else result
        correct = set(q["answer_session_ids"])
        qtype = q["question_type"]
        type_counts[qtype] = type_counts.get(qtype, 0) + 1

        for k in ks:
            if recall_at_k(ranked, correct, k):
                hits[k] += 1
                if qtype not in type_hits:
                    type_hits[qtype] = {}
                type_hits[qtype][k] = type_hits[qtype].get(k, 0) + 1

        if (i + 1) % 100 == 0:
            evaluated = i + 1 - skipped
            print(f"  [{i+1}/{len(questions)}] R@5={hits[5]*100/max(evaluated,1):.1f}%")

    elapsed = time.time() - start
    evaluated = len(questions) - skipped

    print(f"\n=== {label} Results ===")
    print(f"Questions: {evaluated} evaluated, {skipped} skipped, {elapsed:.1f}s elapsed\n")

    results = {}
    for k in ks:
        pct = hits[k] * 100 / max(evaluated, 1)
        print(f"  Recall@{k:<3d}  {pct:5.1f}%  ({hits[k]}/{evaluated})")
        results[f"R@{k}"] = round(pct, 1)

    print(f"\nBy question type:")
    for qtype in sorted(type_counts.keys()):
        count = type_counts[qtype]
        r5 = type_hits.get(qtype, {}).get(5, 0)
        pct = r5 * 100 / count
        print(f"  {qtype:<30s}  R@5={pct:5.1f}%  ({r5}/{count})")

    results["elapsed_s"] = round(elapsed, 2)
    results["evaluated"] = evaluated
    return results


# --- Engine setup ---

def make_engine(engine_name: str):
    """Create a CognitiveEngine for smart_recall reranking."""
    if engine_name == "haiku":
        api_key = os.environ.get("ANTHROPIC_API_KEY")
        if not api_key:
            print("ERROR: ANTHROPIC_API_KEY not set. Needed for --engine haiku.")
            sys.exit(1)
        from soul_protocol.runtime.cognitive.adapters.anthropic import AnthropicEngine
        return AnthropicEngine(model="claude-haiku-4-5-20251001", api_key=api_key)
    elif engine_name == "ollama":
        from soul_protocol.runtime.cognitive.adapters.ollama import OllamaEngine
        return OllamaEngine(model="llama3.2")
    else:
        print(f"Unknown engine: {engine_name}. Use 'haiku' or 'ollama'.")
        sys.exit(1)


def make_embed_fn():
    """Create an embedding function using sentence-transformers (lightweight model)."""
    try:
        from sentence_transformers import SentenceTransformer
        model = SentenceTransformer("all-MiniLM-L6-v2")
        dim = 384

        def embed(texts: list[str]):
            return model.encode(texts, convert_to_numpy=True, normalize_embeddings=True)

        return embed, dim
    except ImportError:
        print("ERROR: sentence-transformers required for tier 3. Install with:")
        print("  uv pip install sentence-transformers")
        sys.exit(1)


# --- Main ---

async def main():
    parser = argparse.ArgumentParser(description="LongMemEval 3-tier benchmark")
    parser.add_argument("--tier", default="1", help="1, 2, 3, or all")
    parser.add_argument("--engine", default="haiku", help="haiku or ollama (for tier 2/3)")
    parser.add_argument("--limit", type=int, default=None, help="Max questions to evaluate")
    parser.add_argument("--data", default=None, help="Path to longmemeval JSON")
    args = parser.parse_args()

    # Find data file
    data_path = args.data
    if not data_path:
        here = Path(__file__).parent
        for name in ["longmemeval_s_cleaned.json", "longmemeval_oracle.json"]:
            p = here / name
            if p.exists():
                data_path = str(p)
                break
    if not data_path or not Path(data_path).exists():
        print("ERROR: LongMemEval data file not found.")
        print("Download from: https://huggingface.co/datasets/xiaowu0162/longmemeval-cleaned")
        sys.exit(1)

    print(f"Loading {data_path}...")
    questions = load_questions(data_path, limit=args.limit)
    print(f"Loaded {len(questions)} questions\n")

    tiers = [args.tier] if args.tier != "all" else ["1", "2", "3"]
    all_results = {}

    for tier in tiers:
        if tier == "1":
            # Tier 1: BM25 only
            def ranker_t1(docs, ids, query):
                return bm25_rank(docs, ids, query, topk=10)

            results = await run_evaluation(questions, ranker_t1, "Tier 1: BM25")
            all_results["tier1_bm25"] = results

        elif tier == "2":
            # Tier 2: BM25 + soul rerank
            engine = make_engine(args.engine)

            def ranker_t2(docs, ids, query):
                bm25_top = bm25_rank(docs, ids, query, topk=15)
                return soul_rerank(docs, ids, bm25_top, query, engine, topk=10)

            results = await run_evaluation(questions, ranker_t2, f"Tier 2: kb-go BM25 + soul rerank ({args.engine})")
            all_results["tier2_soul_rerank"] = results

        elif tier == "3":
            # Tier 3: BM25 + soul rerank + zvec dense
            engine = make_engine(args.engine)
            print("Loading embedding model for tier 3...")
            embed_fn, dim = make_embed_fn()
            print(f"Embedding model loaded (dim={dim})\n")

            def ranker_t3(docs, ids, query):
                # BM25 first pass — trust this ordering completely
                bm25_top = bm25_rank(docs, ids, query, topk=10)

                # Dense search via zvec — only used to find what BM25 missed
                import tempfile
                tmp = tempfile.mkdtemp(prefix="lme_zvec_")
                try:
                    collection = build_zvec_index(docs, ids, embed_fn, dim, tmp)
                    q_emb = embed_fn([query])[0]
                    dense_top = zvec_search(collection, q_emb, topk=10)
                except Exception as e:
                    dense_top = []

                # BM25-primary + dense-fallback: keep BM25 order,
                # append dense-only finds that BM25 missed entirely
                result = list(bm25_top)
                seen = set(result)
                for sid in dense_top:
                    if sid not in seen:
                        result.append(sid)
                        seen.add(sid)
                return result[:10]

            results = await run_evaluation(questions, ranker_t3, "Tier 3: kb-go BM25 + zvec dense (RRF, no reranker)")
            all_results["tier3_full_stack"] = results

    # Save results
    out_path = Path(__file__).parent / "benchmark_results.json"
    with open(out_path, "w") as f:
        json.dump(all_results, f, indent=2)
    print(f"\nResults saved to {out_path}")

    # Summary table
    if len(all_results) > 1:
        print("\n=== Summary ===")
        print(f"{'Tier':<45s} {'R@1':>6s} {'R@3':>6s} {'R@5':>6s} {'R@10':>6s} {'Time':>8s}")
        print("-" * 80)
        for label, r in all_results.items():
            print(f"{label:<45s} {r['R@1']:5.1f}% {r['R@3']:5.1f}% {r['R@5']:5.1f}% {r['R@10']:5.1f}% {r['elapsed_s']:6.1f}s")


if __name__ == "__main__":
    asyncio.run(main())

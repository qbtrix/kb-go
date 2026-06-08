---
{
  "title": "Conversation Ingestion and Topic Clustering for kb-go",
  "summary": "convo.go implements the `kb convo` subcommand family, letting users ingest conversation transcripts into the knowledge base without an LLM. It detects transcript formats, extracts named entities and decisions using deterministic heuristics, clusters turns into topic groups, and generates searchable wiki articles per cluster.",
  "concepts": [
    "conversation ingestion",
    "transcript parsing",
    "named entity recognition",
    "decision extraction",
    "topic clustering",
    "BM25 search",
    "idempotency",
    "role normalization",
    "wiki article generation",
    "JSONL",
    "deterministic NER",
    "kb convo"
  ],
  "categories": [
    "knowledge-base",
    "conversation",
    "NLP",
    "CLI"
  ],
  "source_docs": [
    "3f987c15b215fc96"
  ],
  "backlinks": null,
  "word_count": 710,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Purpose

The conversation mode exists because much institutional knowledge lives in chat logs — design discussions, preference statements, architectural decisions — rather than in formal documents. By ingesting those logs, `kb convo` makes that tacit knowledge searchable alongside the rest of the wiki.

A key design constraint is that extraction must work offline and deterministically, without calling an LLM. This keeps ingestion fast, cheap, and reproducible.

## Format Detection

`parseTranscript` tries three formats in order:

1. **JSON array** — standard `[{"role","content"}]` shape, with a ChatGPT-compatible fallback that reads `author.role` and joins `parts[]`.
2. **JSONL** — one JSON object per line, same fields.
3. **Plain text** — lines beginning with `Speaker: message` patterns matched by a regex covering `user`, `assistant`, `human`, `ai`, `claude`, `gpt`, `bot`, etc.

Failing all three returns an error. The cascade order matters: a file that starts with `{` but is actually JSONL would fail the JSON-array parse and fall through correctly.

## Role Normalization

`normalizeRole` maps variant names (`human` → `user`, `ai`/`claude`/`gpt` → `assistant`) to a two-value canonical set. This matters downstream because article generation checks role to decide whether to include turns in decision extraction — assistant turns are skipped for preference/decision extraction to avoid crediting the AI's suggestions as user decisions.

## Idempotency Guard

`makeSession` computes a SHA-256 hash over the concatenated role+content of all turns. The session ID embeds the first 8 bytes of that hash (`convo-<hex>`). Ingesting the same file twice produces the same session ID, so `saveArticle` will overwrite rather than duplicate. Without this guard, repeated ingestion would bloat the knowledge base with duplicate articles.

## Entity Extraction

`extractEntities` runs a two-pass scan:

- **Technology names**: matched against a hardcoded allowlist of ~80 well-known terms (`python`, `fastapi`, `postgres`, `docker`, etc.) — these are recognized even in lowercase since they never appear capitalized in normal English.
- **Proper nouns and acronyms**: tokens that start with a capital letter (or are all-caps ≥ 2 chars) and are not common English words get classified by `guessEntityType` as person, project, organization, or unknown.

Counts across all occurrences accumulate so the most-mentioned entities rank highest.

## Decision / Preference Extraction

`extractDecisions` scans user turns only for sentences matching three pattern families:

- **Decision**: phrases like "we decided", "we're going with", "we chose".
- **Preference**: "I prefer", "we want", "let's use".
- **Event**: "we shipped", "we deployed", "we launched".

Each match is stored with its turn index so it can later be associated with the right topic cluster.

## Topic Clustering

`clusterTopics` builds a graph where two turns are in the same cluster if they share at least one entity. It uses a simple union-find–style merge: new turns extend the first cluster whose entity set overlaps theirs, otherwise a new cluster is created. The cluster label is the most-frequent entity in the group.

This greedy approach is O(turns × clusters) but is fast enough for typical conversation lengths (hundreds of turns). Very long or entity-sparse conversations may produce a single large cluster.

## Article Generation

`generateConvoArticles` emits one `WikiArticle` per cluster. Each article contains:

- The verbatim conversation turns for that cluster under a `## Conversation` heading.
- Extracted decisions/preferences for those turns under `## Extracted`.
- Concepts drawn from the cluster's entity list (capped at 10).
- Category `"conversation"` so the `convo search` and `convo list` commands can filter efficiently.

Article IDs embed both the session ID and cluster ID, ensuring stability across re-ingestion.

## CLI Surface

| Command | Description |
|---|---|
| `kb convo ingest <file>` | Parse, extract, cluster, save. Accepts `--scope` and `--json`. |
| `kb convo search <query>` | BM25 search restricted to conversation articles. |
| `kb convo list` | List all ingested conversation articles in scope. |

`firstNonFlag` parses the positional argument safely by skipping flags and their values, preventing a flag value like `--scope myfile.json` from being mistaken for the file path.

## Known Gaps

- The clustering algorithm is greedy and single-pass; it can produce unbalanced clusters for entity-dense conversations.
- Plain-text parsing does not handle multi-paragraph turns that span blank lines well — a blank line mid-turn may flush the current speaker prematurely.
- There is no deduplication at the entity level: "postgres" and "postgresql" are treated as separate entities despite the tech-name allowlist containing both.
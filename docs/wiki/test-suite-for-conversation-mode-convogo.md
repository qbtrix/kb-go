---
{
  "title": "Test Suite for Conversation Mode (convo.go)",
  "summary": "convo_test.go contains unit and integration tests for every layer of the conversation pipeline: format parsing, role normalization, entity extraction, decision extraction, topic clustering, and end-to-end article generation. Tests are written in standard Go testing style with table-driven cases and use no external dependencies beyond the standard library.",
  "concepts": [
    "unit testing",
    "table-driven tests",
    "role normalization",
    "entity extraction",
    "decision extraction",
    "topic clustering",
    "end-to-end test",
    "conversation pipeline",
    "test coverage",
    "Go testing"
  ],
  "categories": [
    "testing",
    "conversation",
    "knowledge-base",
    "test"
  ],
  "source_docs": [
    "dba062e1b63ecf37"
  ],
  "backlinks": null,
  "word_count": 497,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Purpose

The test suite validates the deterministic extraction pipeline that underpins `kb convo`. Because extraction uses heuristics rather than an LLM, correctness is easily testable — a given input must always produce the same output — and the tests serve as a living specification of what the heuristics guarantee.

## Transcript Parsing Tests

`TestParseTranscript_JSONArray`, `_JSONL`, and `_PlainText` each verify that a known input in a specific format produces the expected number of turns with correct roles. They also implicitly test format auto-detection order.

`TestParseTranscript_BadInput` verifies that an empty byte slice returns a non-nil error. This prevents silent ingestion of corrupt or empty files that would produce zero-article sessions and confuse users.

`TestNormalizeRole` is a table-driven exhaustive test over 11 role aliases. It guards against regressions when new aliases are added — for example, ensuring `"claude"` maps to `"assistant"` rather than remaining as-is.

## Entity Extraction Tests

Four distinct properties are tested:

- **Tech name recognition** (`TestExtractEntities_TechNames`): verifies that lowercase technology names like `python`, `fastapi`, and `postgres` are found despite not being capitalized.
- **Proper noun detection** (`TestExtractEntities_ProperNouns`): verifies that capitalized names like `Marcus` and `Orion` are found via the capital-letter heuristic.
- **Acronym detection** (`TestExtractEntities_Acronyms`): verifies that all-caps tokens like `API`, `JWT`, and `SSL` are extracted.
- **Noise rejection** (`TestExtractEntities_SkipsCommonWords`, `TestExtractEntities_Empty`): ensures common English words and empty input produce no entities. Without these, stop-word noise would pollute every topic cluster.

The helper `entityNames` converts `[]ExtractedEntity` to a `[]string` for readable error messages.

## Decision Extraction Tests

`TestExtractDecisions_Decisions`, `_Preferences`, and `_Events` each seed a `ConvoTurn` slice with known trigger phrases and assert that the correct `ExtractedDecision.Type` is returned.

`TestExtractDecisions_SkipsAssistantTurns` is particularly important: it verifies that sentences in assistant turns are never classified as decisions. Without this guard, an assistant saying "We could go with Postgres" would be recorded as a user decision, polluting the knowledge base with the assistant's suggestions rather than the user's choices.

## Clustering Tests

`TestClusterTopics_SharedEntities` creates a session where two user turns mention the same entity (`Postgres`) and verifies they land in the same cluster. This confirms the basic union-find behavior.

`TestClusterTopics_EmptySession` verifies that an empty session produces an empty cluster list rather than panicking, which would happen if the code iterated over a nil slice.

## End-to-End Tests

`TestGenerateConvoArticles_CreatesArticles` verifies that at least one `WikiArticle` is produced from a minimal session, that the article has a non-empty title and content, and that the category `"conversation"` is present.

`TestConvoPipeline_EndToEnd` is the full integration test: it parses a three-format transcript, extracts entities and decisions, clusters topics, generates articles, and asserts that a known tech name appears in at least one article's concept list. This catches regressions where a stage produces output but passes incorrect data to the next stage.

## Known Gaps

- There are no tests for `makeSession` idempotency — ingesting the same content twice is not directly tested to produce the same session ID.
- Plain-text multi-paragraph turn parsing is not covered by a dedicated test, leaving the blank-line flush behavior untested.
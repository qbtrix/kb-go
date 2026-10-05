// Implements `kb delete`.

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// cmdDelete removes a single article from a scope so it is no longer
// retrievable. It updates every derived structure that references the id:
// the on-disk wiki file, the raw docs it was compiled from (article.SourceDocs
// are hash-keyed, distinct from the article's slug id), the knowledge index (Articles map + Concepts
// graph + Categories set), the BM25 search cache, the compile hash cache,
// and the vector index. The operation is idempotent — deleting an id that is
// already gone succeeds as a no-op and exits 0, so a purge-on-hide caller may
// safely retry. The id is validated through containedID (same guard the
// article/vector readers use, issue #23) before any path is joined, so a
// traversal id like "../../etc/passwd" is refused rather than escaping the
// scope dir.
func cmdDelete(args []string) {
	if len(args) < 1 {
		fatal("Usage: kb delete <article_id> [--scope NAME] [--json]")
	}
	id := args[0]
	scope := flagStr(args, "--scope", "default")
	jsonOut := flagBool(args, "--json")

	// Refuse traversal ids before joining them into any path. Mirrors the
	// containment guard in loadArticle / the vector readers.
	if err := containedID(id); err != nil {
		fatal("%v", err)
	}

	rawPath := filepath.Join(scopeDir(scope), "raw", id+".json")
	wikiPath := filepath.Join(scopeDir(scope), "wiki", id+".md")

	// Load the article first (before deleting its wiki file) so we know which
	// raw docs it was compiled from. The raw doc is stored under a content-hash
	// id (article.SourceDocs), NOT under the article's slug id — every ingest /
	// build / convo path names them independently — so deleting raw/{id}.json
	// alone would orphan the actual raw doc. Missing/unreadable article is fine
	// (idempotent case); we still clean up whatever else is present.
	art, _ := loadArticle(scope, id)

	// "existed" is true if the id is present anywhere: the index, or either
	// on-disk file. This drives the idempotent no-op message + JSON flag.
	idx := loadIndex(scope)
	_, inIndex := idx.Articles[id]
	rawExists := fileExists(rawPath)
	wikiExists := fileExists(wikiPath)
	existed := inIndex || rawExists || wikiExists || art != nil

	// 1. Remove from the index Articles map.
	delete(idx.Articles, id)

	// 2. Drop the id from every concept's article list; remove concepts that
	//    become empty so the graph carries no dangling references.
	for key, concept := range idx.Concepts {
		if concept == nil {
			delete(idx.Concepts, key)
			continue
		}
		concept.Articles = removeString(concept.Articles, id)
		if len(concept.Articles) == 0 {
			delete(idx.Concepts, key)
		}
	}

	// 3. Delete the on-disk raw + wiki files (best-effort; a missing file is
	//    not an error — it's the idempotent case). Remove the raw docs the
	//    article was compiled from (SourceDocs, hash-keyed) plus the
	//    conventional raw/{id}.json for the rare case where they coincide. Every
	//    raw id is funnelled through containedID so a tampered SourceDocs entry
	//    can't escape the scope's raw dir.
	rawIDs := map[string]bool{id: true}
	if art != nil {
		for _, rid := range art.SourceDocs {
			if rid != "" {
				rawIDs[rid] = true
			}
		}
	}
	for rid := range rawIDs {
		if err := containedID(rid); err != nil {
			continue // refuse to join a path-like raw id; skip it
		}
		p := filepath.Join(scopeDir(scope), "raw", rid+".json")
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			fatal("failed to remove raw doc %s: %v", rid, err)
		}
	}
	if err := os.Remove(wikiPath); err != nil && !os.IsNotExist(err) {
		fatal("failed to remove wiki article %s: %v", id, err)
	}

	// 4. Recompute the Categories set from the articles that remain on disk.
	//    The index Articles map only stores title+summary, so categories can't
	//    be recomputed from it — scanning the surviving wiki files (which no
	//    longer include the deleted id) matches rebuildIndex's category logic
	//    exactly and guarantees no dangling category references.
	remaining, _ := listArticles(scope)
	catSet := map[string]bool{}
	for _, a := range remaining {
		for _, cat := range a.Categories {
			catSet[cat] = true
		}
	}
	idx.Categories = nil
	for cat := range catSet {
		idx.Categories = append(idx.Categories, cat)
	}
	sort.Strings(idx.Categories)

	if err := saveIndex(scope, idx); err != nil {
		fatal("failed to save index: %v", err)
	}

	// 5. Invalidate the caches so stale results don't survive.
	//    a) The BM25 search cache rebuilds on the next search — just remove it.
	searchCache := filepath.Join(scopeDir(scope), "cache", "search_index.json")
	if err := os.Remove(searchCache); err != nil && !os.IsNotExist(err) {
		fatal("failed to invalidate search cache: %v", err)
	}
	//    b) Drop the compile-hash entry that maps to this article id (the hash
	//       cache is keyed by source path, so match on ArticleID).
	cache := loadCache(scope)
	hashChanged := false
	for k, entry := range cache.Files {
		if entry.ArticleID == id {
			delete(cache.Files, k)
			hashChanged = true
		}
	}
	if hashChanged {
		if err := saveCache(scope, cache); err != nil {
			fatal("failed to update hash cache: %v", err)
		}
	}
	//    c) Drop the vector entry so hybrid/vector search can't surface it.
	if vidx, err := loadOrCreateVectorIndex(scope); err == nil {
		if vidx.Remove(id) {
			if err := saveVectorIndex(scope, vidx); err != nil {
				fatal("failed to update vector index: %v", err)
			}
		}
	}

	if jsonOut {
		printJSON(map[string]any{
			"deleted": id,
			"scope":   scope,
			"existed": existed,
		})
	} else if existed {
		fmt.Printf("Deleted article %s from scope %s\n", id, scope)
	} else {
		fmt.Printf("Article %s not found in scope %s (nothing to delete)\n", id, scope)
	}
}

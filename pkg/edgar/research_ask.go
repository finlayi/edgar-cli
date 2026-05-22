package edgar

import (
	"context"
	"strings"
)

func normalizeForm(value string) string {
	normalized := strings.ToUpper(strings.TrimSpace(value))
	if normalized == "" {
		return ""
	}
	return normalized
}

func runResearchAsk(ctx context.Context, params ResearchAskParams, context CommandContext) (CommandResult, error) {
	docPaths, err := loadDocPaths(params.Docs, params.ManifestPath)
	if err != nil {
		return CommandResult{}, err
	}
	if len(docPaths) == 0 {
		return CommandResult{}, NewCLIError(ErrorDocsRequired, "At least one document is required. Pass --doc <path> and/or --manifest <path>.")
	}
	searchData, err := runLexicalSearch(params.Query, docPaths, params.TopK, params.ChunkLines, params.ChunkOverlap)
	if err != nil {
		return CommandResult{}, err
	}
	return CommandResult{Data: searchData}, nil
}

func runResearchAskByID(ctx context.Context, params ResearchAskByIDParams, context CommandContext) (CommandResult, error) {
	cacheRoot := resolveCacheRoot(params.CacheDir, context.Env)
	entity, err := resolveEntity(ctx, params.ID, context.SecClient, false)
	if err != nil {
		return CommandResult{}, err
	}
	form := normalizeForm(params.Scope.Form)

	if form != "" || params.Scope.Latest > 0 {
		latest := params.Scope.Latest
		listResult, err := listFilingRows(ctx, FilingsListParams{
			ID:         entity.CIK,
			Form:       form,
			QueryLimit: latest,
		}, context)
		if err != nil {
			return CommandResult{}, err
		}
		selectedRows := listResult.Rows
		if len(selectedRows) == 0 {
			formLabel := form
			if formLabel == "" {
				formLabel = "any form"
			}
			return CommandResult{}, NewCLIError(ErrorNotFound, "No filings found for "+params.ID+" using "+formLabel+".")
		}
		materialized, err := materializeCachedDocs(ctx, materializeCachedDocsParams{
			CacheRoot: cacheRoot,
			CIK:       entity.CIK,
			Rows:      selectedRows,
			Refresh:   params.Refresh,
			Context:   context,
		})
		if err != nil {
			return CommandResult{}, err
		}
		if len(materialized.Docs) == 0 {
			return CommandResult{}, NewCLIError(ErrorDocsRequired, "No queryable filings were fetched for "+params.ID+".")
		}
		docPaths := cachedDocPaths(materialized.Docs)
		searchData, err := runLexicalSearch(params.Query, docPaths, params.TopK, params.ChunkLines, params.ChunkOverlap)
		if err != nil {
			return CommandResult{}, err
		}
		addEntitySearchContext(searchData, params.ID, entity, cacheRoot)
		searchData["scope"] = map[string]any{"form": nullableString(form), "latest": nullableInt(latest)}
		searchData["corpus_docs_count"] = len(materialized.Docs)
		searchData["selected_filings"] = materialized.Docs
		searchData["sync"] = materializedSyncData(materialized)
		return CommandResult{Data: searchData}, nil
	}

	var manifest *CachedManifest
	if !params.Refresh {
		manifest, err = readCachedManifest(cacheRoot, entity.CIK, params.Profile)
		if err != nil {
			return CommandResult{}, err
		}
	}

	var syncData map[string]any
	if manifest == nil || len(manifest.Docs) == 0 {
		outcome, err := syncResearchProfile(ctx, ResearchSyncParams{
			ID:       params.ID,
			Profile:  params.Profile,
			CacheDir: params.CacheDir,
			Refresh:  params.Refresh,
		}, context)
		if err != nil {
			return CommandResult{}, err
		}
		syncData = syncResultSummary(outcome.Result)
		manifest = &outcome.Manifest
	}

	if manifest == nil || len(manifest.Docs) == 0 {
		return CommandResult{}, NewCLIError(ErrorDocsRequired, "No cached documents found for "+params.ID+" profile "+string(params.Profile)+". Run research sync first.")
	}
	docPaths := cachedDocPaths(manifest.Docs)
	searchData, err := runLexicalSearch(params.Query, docPaths, params.TopK, params.ChunkLines, params.ChunkOverlap)
	if err != nil {
		return CommandResult{}, err
	}
	addEntitySearchContext(searchData, params.ID, entity, cacheRoot)
	searchData["profile"] = params.Profile
	searchData["corpus_docs_count"] = len(manifest.Docs)
	searchData["manifest_docs"] = manifest.Docs
	if syncData != nil {
		searchData["sync"] = syncData
	}
	return CommandResult{Data: searchData}, nil
}

func cachedDocPaths(docs []CachedDoc) []string {
	paths := make([]string, 0, len(docs))
	for _, doc := range docs {
		paths = append(paths, doc.Path)
	}
	return paths
}

func addEntitySearchContext(searchData map[string]any, id string, entity ResolvedEntity, cacheRoot string) {
	searchData["id"] = id
	searchData["cik"] = entity.CIK
	searchData["ticker"] = entity.Ticker
	searchData["title"] = entity.Title
	searchData["cache_root"] = cacheRoot
}

func materializedSyncData(materialized materializedDocs) map[string]any {
	return map[string]any{
		"fetched_count": materialized.FetchedCount,
		"reused_count":  materialized.ReusedCount,
		"docs_count":    len(materialized.Docs),
		"skipped_count": len(materialized.Skipped),
		"skipped":       materialized.Skipped,
	}
}

func syncResultSummary(result ResearchSyncResult) map[string]any {
	return map[string]any{
		"fetched_count": result.FetchedCount,
		"reused_count":  result.ReusedCount,
		"docs_count":    result.DocsCount,
		"skipped_count": result.SkippedCount,
	}
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableInt(value int) any {
	if value == 0 {
		return nil
	}
	return value
}

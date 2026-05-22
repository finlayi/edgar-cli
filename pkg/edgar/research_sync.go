package edgar

import (
	"context"
	"sort"
)

func syncResearchProfile(ctx context.Context, params ResearchSyncParams, context CommandContext) (researchSyncOutcome, error) {
	entity, err := resolveEntity(ctx, params.ID, context.SecClient, false)
	if err != nil {
		return researchSyncOutcome{}, err
	}
	cacheRoot := resolveCacheRoot(params.CacheDir, context.Env)
	rules := profileRules[params.Profile]
	selectedByAccession := map[string]FilingRow{}
	order := []string{}

	for _, rule := range rules {
		from := ""
		if rule.RecentDays > 0 {
			from = dateDaysAgo(rule.RecentDays)
		}
		listResult, err := listFilingRows(ctx, FilingsListParams{
			ID:         entity.CIK,
			Form:       rule.Form,
			From:       from,
			QueryLimit: rule.QueryLimit,
		}, context)
		if err != nil {
			return researchSyncOutcome{}, err
		}
		for _, row := range listResult.Rows {
			if _, ok := selectedByAccession[row.Accession]; !ok {
				order = append(order, row.Accession)
			}
			selectedByAccession[row.Accession] = row
		}
	}

	selectedRows := make([]FilingRow, 0, len(order))
	for _, accession := range order {
		selectedRows = append(selectedRows, selectedByAccession[accession])
	}
	sort.SliceStable(selectedRows, func(i, j int) bool {
		return pointerStringValue(selectedRows[i].FilingDate) > pointerStringValue(selectedRows[j].FilingDate)
	})

	materialized, err := materializeCachedDocs(ctx, materializeCachedDocsParams{
		CacheRoot: cacheRoot,
		CIK:       entity.CIK,
		Rows:      selectedRows,
		Refresh:   params.Refresh,
		Context:   context,
	})
	if err != nil {
		return researchSyncOutcome{}, err
	}

	manifest := CachedManifest{
		Version:  1,
		IDInput:  params.ID,
		CIK:      entity.CIK,
		Ticker:   entity.Ticker,
		Title:    entity.Title,
		Profile:  params.Profile,
		SyncedAt: nowISO(),
		Docs:     materialized.Docs,
	}
	manifestPath, err := writeCachedManifest(cacheRoot, manifest)
	if err != nil {
		return researchSyncOutcome{}, err
	}

	return researchSyncOutcome{
		Result: ResearchSyncResult{
			ID:           params.ID,
			CIK:          entity.CIK,
			Ticker:       entity.Ticker,
			Title:        entity.Title,
			Profile:      params.Profile,
			CacheRoot:    cacheRoot,
			ManifestPath: manifestPath,
			DocsCount:    len(materialized.Docs),
			FetchedCount: materialized.FetchedCount,
			ReusedCount:  materialized.ReusedCount,
			SkippedCount: len(materialized.Skipped),
			Skipped:      materialized.Skipped,
			Docs:         materialized.Docs,
		},
		Manifest: manifest,
	}, nil
}

func runResearchSync(ctx context.Context, params ResearchSyncParams, context CommandContext) (CommandResult, error) {
	outcome, err := syncResearchProfile(ctx, params, context)
	if err != nil {
		return CommandResult{}, err
	}
	return CommandResult{Data: outcome.Result}, nil
}

func pointerStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

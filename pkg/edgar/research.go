package edgar

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

type ResearchProfile string

const (
	ResearchProfileCore       ResearchProfile = "core"
	ResearchProfileEvents     ResearchProfile = "events"
	ResearchProfileFinancials ResearchProfile = "financials"
)

type SyncRule struct {
	Form       string
	QueryLimit int
	RecentDays int
}

var profileRules = map[ResearchProfile][]SyncRule{
	ResearchProfileCore: {
		{Form: "10-K", QueryLimit: 1},
		{Form: "10-Q", QueryLimit: 3},
		{Form: "8-K", QueryLimit: 12, RecentDays: 180},
	},
	ResearchProfileEvents: {
		{Form: "8-K", QueryLimit: 24, RecentDays: 365},
	},
	ResearchProfileFinancials: {
		{Form: "10-K", QueryLimit: 2},
		{Form: "10-Q", QueryLimit: 6},
	},
}

type CachedDoc struct {
	Accession  string  `json:"accession"`
	Form       *string `json:"form"`
	FilingDate *string `json:"filing_date"`
	ReportDate *string `json:"report_date"`
	FilingURL  *string `json:"filing_url"`
	Path       string  `json:"path"`
}

type CachedManifest struct {
	Version  int             `json:"version"`
	IDInput  string          `json:"id_input"`
	CIK      string          `json:"cik"`
	Ticker   *string         `json:"ticker"`
	Title    *string         `json:"title"`
	Profile  ResearchProfile `json:"profile"`
	SyncedAt string          `json:"synced_at"`
	Docs     []CachedDoc     `json:"docs"`
}

type ResearchSyncParams struct {
	ID       string
	Profile  ResearchProfile
	CacheDir string
	Refresh  bool
}

type ResearchAskParams struct {
	Query        string
	Docs         []string
	ManifestPath string
	TopK         int
	ChunkLines   int
	ChunkOverlap int
}

type AskScope struct {
	Form   string
	Latest int
}

type ResearchAskByIDParams struct {
	ID           string
	Query        string
	Profile      ResearchProfile
	Scope        AskScope
	CacheDir     string
	Refresh      bool
	TopK         int
	ChunkLines   int
	ChunkOverlap int
}

type Chunk struct {
	DocPath       string
	Accession     *string
	LineStart     int
	LineEnd       int
	Text          string
	TokenCount    int
	TermFrequency map[string]int
}

type materializedDocs struct {
	Docs         []CachedDoc
	FetchedCount int
	ReusedCount  int
	Skipped      []map[string]string
}

var (
	tokenPattern     = regexp.MustCompile(`[a-z0-9]+`)
	accessionInPath  = regexp.MustCompile(`\d{10}-\d{2}-\d{6}`)
	spacePattern     = regexp.MustCompile(`[ \t]+`)
	blankLinePattern = regexp.MustCompile(`\n{3,}`)
)

var queryStopwords = map[string]bool{
	"a": true, "an": true, "and": true, "are": true, "as": true, "at": true,
	"be": true, "by": true, "for": true, "from": true, "how": true, "in": true,
	"into": true, "is": true, "it": true, "its": true, "of": true, "on": true,
	"or": true, "that": true, "the": true, "their": true, "there": true,
	"these": true, "they": true, "this": true, "to": true, "was": true,
	"were": true, "what": true, "when": true, "where": true, "which": true,
	"who": true, "why": true, "with": true,
}

var coverBoilerplatePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)securities registered pursuant to section 12\(b\)`),
	regexp.MustCompile(`(?i)indicate by check mark`),
	regexp.MustCompile(`(?i)commission file number`),
	regexp.MustCompile(`(?i)for the quarterly period ended`),
	regexp.MustCompile(`(?i)for the fiscal year ended`),
}

func parseResearchProfile(value string) (ResearchProfile, error) {
	switch ResearchProfile(strings.ToLower(strings.TrimSpace(value))) {
	case ResearchProfileCore:
		return ResearchProfileCore, nil
	case ResearchProfileEvents:
		return ResearchProfileEvents, nil
	case ResearchProfileFinancials:
		return ResearchProfileFinancials, nil
	default:
		return "", NewCLIError(ErrorValidationRequired, "--profile must be one of core|events|financials")
	}
}

func nowISO() string {
	return time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
}

func dateDaysAgo(days int) string {
	return time.Now().UTC().AddDate(0, 0, -days).Format("2006-01-02")
}

func defaultCacheRoot(env map[string]string) string {
	if value := strings.TrimSpace(env["EDGAR_CACHE_DIR"]); value != "" {
		return absPath(value)
	}
	if value := strings.TrimSpace(env["XDG_CACHE_HOME"]); value != "" {
		return absPath(filepath.Join(value, "edgar-cli"))
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return absPath(filepath.Join(".cache", "edgar-cli"))
	}
	return absPath(filepath.Join(home, ".cache", "edgar-cli"))
}

func resolveCacheRoot(cacheDir string, env map[string]string) string {
	if strings.TrimSpace(cacheDir) != "" {
		return absPath(cacheDir)
	}
	return defaultCacheRoot(env)
}

func companyCacheDir(cacheRoot string, cik string) string {
	return filepath.Join(cacheRoot, "research", "companies", cik)
}

func profileManifestPath(cacheRoot string, cik string, profile ResearchProfile) string {
	return filepath.Join(companyCacheDir(cacheRoot, cik), "profiles", string(profile)+".json")
}

func filingDocPath(cacheRoot string, cik string, accession string) string {
	return filepath.Join(companyCacheDir(cacheRoot, cik), "filings", accession+".md")
}

func absPath(value string) string {
	resolved, err := filepath.Abs(value)
	if err != nil {
		return value
	}
	return resolved
}

func readCachedManifest(cacheRoot string, cik string, profile ResearchProfile) (*CachedManifest, error) {
	manifestPath := profileManifestPath(cacheRoot, cik, profile)
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, NewCLIError(ErrorValidationRequired, "Unable to read cached manifest "+manifestPath+": "+err.Error())
	}
	var manifest CachedManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return nil, NewCLIError(ErrorParse, "Cached manifest is not valid JSON: "+manifestPath)
	}
	if manifest.Version != 1 || manifest.CIK == "" || manifest.Docs == nil {
		return nil, NewCLIError(ErrorParse, "Cached manifest is malformed")
	}
	for _, doc := range manifest.Docs {
		if doc.Path == "" || doc.Accession == "" {
			return nil, NewCLIError(ErrorParse, "Cached manifest is malformed")
		}
	}
	return &manifest, nil
}

func writeCachedManifest(cacheRoot string, manifest CachedManifest) (string, error) {
	manifestPath := profileManifestPath(cacheRoot, manifest.CIK, manifest.Profile)
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
		return "", NewCLIError(ErrorValidationRequired, "Unable to create cache directory "+filepath.Dir(manifestPath)+": "+err.Error())
	}
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", NewCLIError(ErrorInternal, err.Error())
	}
	if err := os.WriteFile(manifestPath, append(encoded, '\n'), 0o644); err != nil {
		return "", NewCLIError(ErrorValidationRequired, "Unable to write cached manifest "+manifestPath+": "+err.Error())
	}
	return manifestPath, nil
}

func fileExists(filePath string) (bool, error) {
	info, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, NewCLIError(ErrorValidationRequired, "Unable to stat "+filePath+": "+err.Error())
	}
	return info.Mode().IsRegular(), nil
}

func materializeCachedDocs(ctx context.Context, params struct {
	CacheRoot string
	CIK       string
	Rows      []FilingRow
	Refresh   bool
	Context   CommandContext
}) (materializedDocs, error) {
	docs := []CachedDoc{}
	skipped := []map[string]string{}
	fetchedCount := 0
	reusedCount := 0

	for _, row := range params.Rows {
		docPath := filingDocPath(params.CacheRoot, params.CIK, row.Accession)
		exists, err := fileExists(docPath)
		if err != nil {
			return materializedDocs{}, err
		}
		shouldUseCache := !params.Refresh && exists
		if !shouldUseCache {
			result, err := runFilingsGet(ctx, FilingsGetParams{
				ID:        params.CIK,
				Accession: row.Accession,
				Format:    "markdown",
			}, params.Context)
			if err != nil {
				if cliErr, ok := err.(*CLIError); ok && cliErr.Code == ErrorNotFound {
					skipped = append(skipped, map[string]string{"accession": row.Accession, "reason": cliErr.Message})
					continue
				}
				return materializedDocs{}, err
			}
			data, ok := result.Data.(map[string]any)
			if !ok {
				return materializedDocs{}, NewCLIError(ErrorParse, "Unable to parse markdown content for accession "+row.Accession)
			}
			content, ok := data["content"].(string)
			if !ok {
				return materializedDocs{}, NewCLIError(ErrorParse, "Unable to parse markdown content for accession "+row.Accession)
			}
			if err := os.MkdirAll(filepath.Dir(docPath), 0o755); err != nil {
				return materializedDocs{}, NewCLIError(ErrorValidationRequired, "Unable to create cache directory "+filepath.Dir(docPath)+": "+err.Error())
			}
			if !strings.HasSuffix(content, "\n") {
				content += "\n"
			}
			if err := os.WriteFile(docPath, []byte(content), 0o644); err != nil {
				return materializedDocs{}, NewCLIError(ErrorValidationRequired, "Unable to write cached document "+docPath+": "+err.Error())
			}
			fetchedCount++
		} else {
			reusedCount++
		}

		docs = append(docs, CachedDoc{
			Accession:  row.Accession,
			Form:       row.Form,
			FilingDate: row.FilingDate,
			ReportDate: row.ReportDate,
			FilingURL:  row.FilingURL,
			Path:       docPath,
		})
	}

	return materializedDocs{Docs: docs, FetchedCount: fetchedCount, ReusedCount: reusedCount, Skipped: skipped}, nil
}

func normalizeForm(value string) string {
	normalized := strings.ToUpper(strings.TrimSpace(value))
	if normalized == "" {
		return ""
	}
	return normalized
}

func tokenize(value string) []string {
	raw := tokenPattern.FindAllString(strings.ToLower(value), -1)
	tokens := make([]string, 0, len(raw))
	for _, token := range raw {
		if len(token) >= 2 {
			tokens = append(tokens, token)
		}
	}
	return tokens
}

func uniqueTokens(tokens []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, token := range tokens {
		if seen[token] {
			continue
		}
		seen[token] = true
		out = append(out, token)
	}
	return out
}

func buildQueryTerms(query string) []string {
	rawTokens := tokenize(query)
	filtered := []string{}
	for _, token := range rawTokens {
		if !queryStopwords[token] {
			filtered = append(filtered, token)
		}
	}
	if len(filtered) > 0 {
		return uniqueTokens(filtered)
	}
	return uniqueTokens(rawTokens)
}

func buildQueryBigrams(queryTerms []string) []string {
	bigrams := []string{}
	for idx := 0; idx < len(queryTerms)-1; idx++ {
		bigrams = append(bigrams, queryTerms[idx]+" "+queryTerms[idx+1])
	}
	return uniqueTokens(bigrams)
}

func buildTermFrequency(tokens []string) map[string]int {
	frequency := map[string]int{}
	for _, token := range tokens {
		frequency[token]++
	}
	return frequency
}

func extractAccession(docPath string) *string {
	match := accessionInPath.FindString(docPath)
	if match == "" {
		return nil
	}
	return &match
}

func loadDocPaths(docs []string, manifestPath string) ([]string, error) {
	fromOptions := []string{}
	for _, docPath := range docs {
		if trimmed := strings.TrimSpace(docPath); trimmed != "" {
			fromOptions = append(fromOptions, trimmed)
		}
	}

	fromManifest := []string{}
	if strings.TrimSpace(manifestPath) != "" {
		resolvedManifestPath := absPath(manifestPath)
		manifestRaw, err := os.ReadFile(resolvedManifestPath)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, NewCLIError(ErrorNotFound, "Manifest not found: "+resolvedManifestPath)
			}
			return nil, NewCLIError(ErrorValidationRequired, "Unable to read manifest "+resolvedManifestPath+": "+err.Error())
		}
		parsed, err := parseManifest(manifestRaw)
		if err != nil {
			return nil, err
		}
		fromManifest = append(fromManifest, parsed...)
	}

	seen := map[string]bool{}
	resolved := []string{}
	for _, docPath := range append(fromOptions, fromManifest...) {
		abs := absPath(docPath)
		if abs == "" || seen[abs] {
			continue
		}
		seen[abs] = true
		resolved = append(resolved, abs)
	}
	return resolved, nil
}

func parseManifest(raw []byte) ([]string, error) {
	var list []string
	if err := json.Unmarshal(raw, &list); err == nil {
		return compactStringList(list), nil
	}
	var object struct {
		Docs []string `json:"docs"`
	}
	if err := json.Unmarshal(raw, &object); err == nil && object.Docs != nil {
		return compactStringList(object.Docs), nil
	}
	var cached struct {
		Docs []CachedDoc `json:"docs"`
	}
	if err := json.Unmarshal(raw, &cached); err == nil && cached.Docs != nil {
		paths := make([]string, 0, len(cached.Docs))
		for _, doc := range cached.Docs {
			paths = append(paths, doc.Path)
		}
		return compactStringList(paths), nil
	}
	return nil, NewCLIError(ErrorValidationRequired, "Manifest must be a JSON array of strings, object with a docs string array, or cached manifest with docs paths")
}

func compactStringList(values []string) []string {
	out := []string{}
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func ensureReadableTextFile(filePath string) (string, error) {
	info, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", NewCLIError(ErrorNotFound, "Document not found: "+filePath)
		}
		return "", NewCLIError(ErrorValidationRequired, "Unable to stat document "+filePath+": "+err.Error())
	}
	if !info.Mode().IsRegular() {
		return "", NewCLIError(ErrorValidationRequired, "Path is not a file: "+filePath)
	}
	raw, err := os.ReadFile(filePath)
	if err != nil {
		return "", NewCLIError(ErrorValidationRequired, "Unable to read document "+filePath+": "+err.Error())
	}
	if strings.Contains(string(raw), "\x00") {
		return "", NewCLIError(ErrorValidationRequired, "File appears to be binary: "+filePath)
	}
	if !utf8.Valid(raw) {
		return "", NewCLIError(ErrorValidationRequired, "File appears to be binary: "+filePath)
	}
	return string(raw), nil
}

func chunkDocument(docPath string, content string, chunkLines int, chunkOverlap int) []Chunk {
	lines := regexp.MustCompile(`\r?\n`).Split(content, -1)
	step := max(1, chunkLines-chunkOverlap)
	chunks := []Chunk{}
	accession := extractAccession(docPath)

	for lineIdx := 0; lineIdx < len(lines); lineIdx += step {
		start := lineIdx
		endExclusive := min(len(lines), start+chunkLines)
		text := strings.TrimSpace(strings.Join(lines[start:endExclusive], "\n"))
		if text == "" {
			if endExclusive >= len(lines) {
				break
			}
			continue
		}
		tokens := tokenize(text)
		chunks = append(chunks, Chunk{
			DocPath:       docPath,
			Accession:     accession,
			LineStart:     start + 1,
			LineEnd:       endExclusive,
			Text:          text,
			TokenCount:    len(tokens),
			TermFrequency: buildTermFrequency(tokens),
		})
		if endExclusive >= len(lines) {
			break
		}
	}
	return chunks
}

func bm25Score(queryTerms []string, chunk Chunk, docFrequencyByTerm map[string]int, totalChunkCount int, averageChunkLength float64) float64 {
	const k1 = 1.2
	const b = 0.75
	score := 0.0
	for _, term := range queryTerms {
		tf := chunk.TermFrequency[term]
		if tf == 0 {
			continue
		}
		df := docFrequencyByTerm[term]
		idf := math.Log(1 + (float64(totalChunkCount)-float64(df)+0.5)/(float64(df)+0.5))
		normalizedLength := 1.0
		if averageChunkLength > 0 {
			normalizedLength = float64(chunk.TokenCount) / averageChunkLength
		}
		denominator := float64(tf) + k1*(1-b+b*normalizedLength)
		score += idf * ((float64(tf) * (k1 + 1)) / denominator)
	}
	return score
}

func adjustedChunkScore(chunk Chunk, baseScore float64, queryTerms []string, queryBigrams []string) float64 {
	if baseScore <= 0 {
		return 0
	}
	termHits := countTermHits(queryTerms, chunk.TermFrequency)
	if len(queryTerms) >= 3 && termHits < 2 {
		return 0
	}
	coverage := float64(termHits) / float64(max(1, len(queryTerms)))
	bigramHits := countBigramHits(chunk.Text, queryBigrams)
	multiplier := 1.0

	if coverage >= 1 {
		multiplier *= 1.25
	} else if coverage >= 0.7 {
		multiplier *= 1.15
	} else if coverage >= 0.5 {
		multiplier *= 1.08
	} else if len(queryTerms) >= 3 && coverage <= 0.25 {
		multiplier *= 0.8
	}
	if bigramHits > 0 {
		multiplier *= 1 + math.Min(0.24, float64(bigramHits)*0.08)
	}
	if looksLikeCoverBoilerplate(chunk) {
		multiplier *= 0.45
	}
	return baseScore * multiplier
}

func countTermHits(queryTerms []string, termFrequency map[string]int) int {
	hits := 0
	for _, term := range queryTerms {
		if termFrequency[term] > 0 {
			hits++
		}
	}
	return hits
}

func countBigramHits(chunkText string, queryBigrams []string) int {
	text := strings.ToLower(chunkText)
	hits := 0
	for _, bigram := range queryBigrams {
		if strings.Contains(text, bigram) {
			hits++
		}
	}
	return hits
}

func looksLikeCoverBoilerplate(chunk Chunk) bool {
	if chunk.LineStart > 140 {
		return false
	}
	for _, pattern := range coverBoilerplatePatterns {
		if pattern.MatchString(chunk.Text) {
			return true
		}
	}
	return false
}

func compactWhitespace(value string) string {
	value = spacePattern.ReplaceAllString(value, " ")
	value = blankLinePattern.ReplaceAllString(value, "\n\n")
	return strings.TrimSpace(value)
}

func trimExcerpt(value string, maxChars int) string {
	if len(value) <= maxChars {
		return value
	}
	return strings.TrimRight(value[:max(0, maxChars-3)], " ") + "..."
}

func runResearchSync(ctx context.Context, params ResearchSyncParams, context CommandContext) (CommandResult, error) {
	entity, err := resolveEntity(ctx, params.ID, context.SecClient, false)
	if err != nil {
		return CommandResult{}, err
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
		listResult, err := runFilingsList(ctx, FilingsListParams{
			ID:         entity.CIK,
			Form:       rule.Form,
			From:       from,
			QueryLimit: rule.QueryLimit,
		}, context)
		if err != nil {
			return CommandResult{}, err
		}
		rows := listResult.Data.([]FilingRow)
		for _, row := range rows {
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

	materialized, err := materializeCachedDocs(ctx, struct {
		CacheRoot string
		CIK       string
		Rows      []FilingRow
		Refresh   bool
		Context   CommandContext
	}{
		CacheRoot: cacheRoot,
		CIK:       entity.CIK,
		Rows:      selectedRows,
		Refresh:   params.Refresh,
		Context:   context,
	})
	if err != nil {
		return CommandResult{}, err
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
		return CommandResult{}, err
	}

	return CommandResult{Data: map[string]any{
		"id":            params.ID,
		"cik":           entity.CIK,
		"ticker":        entity.Ticker,
		"title":         entity.Title,
		"profile":       params.Profile,
		"cache_root":    cacheRoot,
		"manifest_path": manifestPath,
		"docs_count":    len(materialized.Docs),
		"fetched_count": materialized.FetchedCount,
		"reused_count":  materialized.ReusedCount,
		"skipped_count": len(materialized.Skipped),
		"skipped":       materialized.Skipped,
		"docs":          materialized.Docs,
	}}, nil
}

func runResearchAsk(ctx context.Context, params ResearchAskParams, context CommandContext) (CommandResult, error) {
	docPaths, err := loadDocPaths(params.Docs, params.ManifestPath)
	if err != nil {
		return CommandResult{}, err
	}
	if len(docPaths) == 0 {
		return CommandResult{}, NewCLIError(ErrorDocsRequired, "At least one document is required. Pass --doc <path> and/or --manifest <path>.")
	}
	return runLexicalSearch(params.Query, docPaths, params.TopK, params.ChunkLines, params.ChunkOverlap)
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
		listResult, err := runFilingsList(ctx, FilingsListParams{
			ID:         entity.CIK,
			Form:       form,
			QueryLimit: latest,
		}, context)
		if err != nil {
			return CommandResult{}, err
		}
		selectedRows := listResult.Data.([]FilingRow)
		if len(selectedRows) == 0 {
			formLabel := form
			if formLabel == "" {
				formLabel = "any form"
			}
			return CommandResult{}, NewCLIError(ErrorNotFound, "No filings found for "+params.ID+" using "+formLabel+".")
		}
		materialized, err := materializeCachedDocs(ctx, struct {
			CacheRoot string
			CIK       string
			Rows      []FilingRow
			Refresh   bool
			Context   CommandContext
		}{
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
		docPaths := make([]string, 0, len(materialized.Docs))
		for _, doc := range materialized.Docs {
			docPaths = append(docPaths, doc.Path)
		}
		searchResult, err := runLexicalSearch(params.Query, docPaths, params.TopK, params.ChunkLines, params.ChunkOverlap)
		if err != nil {
			return CommandResult{}, err
		}
		searchData := searchResult.Data.(map[string]any)
		searchData["id"] = params.ID
		searchData["cik"] = entity.CIK
		searchData["ticker"] = entity.Ticker
		searchData["title"] = entity.Title
		searchData["cache_root"] = cacheRoot
		searchData["scope"] = map[string]any{"form": nullableString(form), "latest": nullableInt(latest)}
		searchData["corpus_docs_count"] = len(materialized.Docs)
		searchData["selected_filings"] = materialized.Docs
		searchData["sync"] = map[string]any{
			"fetched_count": materialized.FetchedCount,
			"reused_count":  materialized.ReusedCount,
			"docs_count":    len(materialized.Docs),
			"skipped_count": len(materialized.Skipped),
			"skipped":       materialized.Skipped,
		}
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
		syncResult, err := runResearchSync(ctx, ResearchSyncParams{
			ID:       params.ID,
			Profile:  params.Profile,
			CacheDir: params.CacheDir,
			Refresh:  params.Refresh,
		}, context)
		if err != nil {
			return CommandResult{}, err
		}
		syncPayload := syncResult.Data.(map[string]any)
		syncData = map[string]any{
			"fetched_count": numberFromMap(syncPayload, "fetched_count"),
			"reused_count":  numberFromMap(syncPayload, "reused_count"),
			"docs_count":    numberFromMap(syncPayload, "docs_count"),
			"skipped_count": numberFromMap(syncPayload, "skipped_count"),
		}
		manifest, err = readCachedManifest(cacheRoot, entity.CIK, params.Profile)
		if err != nil {
			return CommandResult{}, err
		}
	}

	if manifest == nil || len(manifest.Docs) == 0 {
		return CommandResult{}, NewCLIError(ErrorDocsRequired, "No cached documents found for "+params.ID+" profile "+string(params.Profile)+". Run research sync first.")
	}
	docPaths := make([]string, 0, len(manifest.Docs))
	for _, doc := range manifest.Docs {
		docPaths = append(docPaths, doc.Path)
	}
	searchResult, err := runLexicalSearch(params.Query, docPaths, params.TopK, params.ChunkLines, params.ChunkOverlap)
	if err != nil {
		return CommandResult{}, err
	}
	searchData := searchResult.Data.(map[string]any)
	searchData["id"] = params.ID
	searchData["cik"] = entity.CIK
	searchData["ticker"] = entity.Ticker
	searchData["title"] = entity.Title
	searchData["cache_root"] = cacheRoot
	searchData["profile"] = params.Profile
	searchData["corpus_docs_count"] = len(manifest.Docs)
	searchData["manifest_docs"] = manifest.Docs
	if syncData != nil {
		searchData["sync"] = syncData
	}
	return CommandResult{Data: searchData}, nil
}

func runLexicalSearch(query string, docPaths []string, topK int, chunkLines int, chunkOverlap int) (CommandResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return CommandResult{}, NewCLIError(ErrorValidationRequired, "Query must not be empty")
	}
	if chunkOverlap >= chunkLines {
		return CommandResult{}, NewCLIError(ErrorValidationRequired, "--chunk-overlap must be less than --chunk-lines")
	}

	type docInfo struct {
		Path      string
		Bytes     int
		LineCount int
		Chunks    []Chunk
	}
	docs := []docInfo{}
	allChunks := []Chunk{}
	for _, docPath := range docPaths {
		content, err := ensureReadableTextFile(docPath)
		if err != nil {
			return CommandResult{}, err
		}
		chunks := chunkDocument(docPath, content, chunkLines, chunkOverlap)
		doc := docInfo{
			Path:      docPath,
			Bytes:     len([]byte(content)),
			LineCount: len(regexp.MustCompile(`\r?\n`).Split(content, -1)),
			Chunks:    chunks,
		}
		docs = append(docs, doc)
		allChunks = append(allChunks, chunks...)
	}

	docSummaries := make([]map[string]any, 0, len(docs))
	for _, doc := range docs {
		docSummaries = append(docSummaries, map[string]any{
			"path":       doc.Path,
			"bytes":      doc.Bytes,
			"line_count": doc.LineCount,
		})
	}
	if len(allChunks) == 0 {
		return CommandResult{Data: map[string]any{
			"query":        query,
			"backend":      "lexical",
			"docs":         docSummaries,
			"result_count": 0,
			"results":      []any{},
		}}, nil
	}

	queryTerms := buildQueryTerms(query)
	if len(queryTerms) == 0 {
		return CommandResult{}, NewCLIError(ErrorValidationRequired, "Query must contain at least one alphanumeric token")
	}
	queryBigrams := buildQueryBigrams(queryTerms)
	docFrequencyByTerm := map[string]int{}
	for _, term := range queryTerms {
		count := 0
		for _, chunk := range allChunks {
			if chunk.TermFrequency[term] > 0 {
				count++
			}
		}
		docFrequencyByTerm[term] = count
	}
	totalTokenCount := 0
	for _, chunk := range allChunks {
		totalTokenCount += chunk.TokenCount
	}
	averageChunkLength := float64(totalTokenCount) / float64(max(1, len(allChunks)))

	type scoredChunk struct {
		Chunk Chunk
		Score float64
	}
	scored := []scoredChunk{}
	for _, chunk := range allChunks {
		baseScore := bm25Score(queryTerms, chunk, docFrequencyByTerm, len(allChunks), averageChunkLength)
		score := adjustedChunkScore(chunk, baseScore, queryTerms, queryBigrams)
		if score > 0 {
			scored = append(scored, scoredChunk{Chunk: chunk, Score: score})
		}
	}
	sort.SliceStable(scored, func(i, j int) bool {
		return scored[i].Score > scored[j].Score
	})
	if len(scored) > topK {
		scored = scored[:topK]
	}

	results := make([]map[string]any, 0, len(scored))
	for idx, item := range scored {
		results = append(results, map[string]any{
			"rank":       idx + 1,
			"score":      math.Round(item.Score*1_000_000) / 1_000_000,
			"path":       item.Chunk.DocPath,
			"accession":  item.Chunk.Accession,
			"line_start": item.Chunk.LineStart,
			"line_end":   item.Chunk.LineEnd,
			"excerpt":    trimExcerpt(compactWhitespace(item.Chunk.Text), 1200),
		})
	}

	return CommandResult{Data: map[string]any{
		"query":        query,
		"backend":      "lexical",
		"query_terms":  queryTerms,
		"docs":         docSummaries,
		"chunk_count":  len(allChunks),
		"result_count": len(scored),
		"results":      results,
	}}, nil
}

func pointerStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
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

func numberFromMap(value map[string]any, key string) int {
	switch typed := value[key].(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return 0
	}
}

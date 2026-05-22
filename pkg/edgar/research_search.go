package edgar

import (
	"encoding/json"
	"math"
	"os"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

var (
	tokenPattern     = regexp.MustCompile(`[a-z0-9]+`)
	accessionInPath  = regexp.MustCompile(`\d{10}-\d{2}-\d{6}`)
	lineBreakPattern = regexp.MustCompile(`\r?\n`)
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
	lines := lineBreakPattern.Split(content, -1)
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

func runLexicalSearch(query string, docPaths []string, topK int, chunkLines int, chunkOverlap int) (map[string]any, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, NewCLIError(ErrorValidationRequired, "Query must not be empty")
	}
	if chunkOverlap >= chunkLines {
		return nil, NewCLIError(ErrorValidationRequired, "--chunk-overlap must be less than --chunk-lines")
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
			return nil, err
		}
		chunks := chunkDocument(docPath, content, chunkLines, chunkOverlap)
		doc := docInfo{
			Path:      docPath,
			Bytes:     len([]byte(content)),
			LineCount: len(lineBreakPattern.Split(content, -1)),
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
		return map[string]any{
			"query":        query,
			"backend":      "lexical",
			"docs":         docSummaries,
			"result_count": 0,
			"results":      []any{},
		}, nil
	}

	queryTerms := buildQueryTerms(query)
	if len(queryTerms) == 0 {
		return nil, NewCLIError(ErrorValidationRequired, "Query must contain at least one alphanumeric token")
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

	return map[string]any{
		"query":        query,
		"backend":      "lexical",
		"query_terms":  queryTerms,
		"docs":         docSummaries,
		"chunk_count":  len(allChunks),
		"result_count": len(scored),
		"results":      results,
	}, nil
}

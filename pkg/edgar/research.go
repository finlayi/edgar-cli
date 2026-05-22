package edgar

import "strings"

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

type ResearchSyncResult struct {
	ID           string              `json:"id"`
	CIK          string              `json:"cik"`
	Ticker       *string             `json:"ticker"`
	Title        *string             `json:"title"`
	Profile      ResearchProfile     `json:"profile"`
	CacheRoot    string              `json:"cache_root"`
	ManifestPath string              `json:"manifest_path"`
	DocsCount    int                 `json:"docs_count"`
	FetchedCount int                 `json:"fetched_count"`
	ReusedCount  int                 `json:"reused_count"`
	SkippedCount int                 `json:"skipped_count"`
	Skipped      []map[string]string `json:"skipped"`
	Docs         []CachedDoc         `json:"docs"`
}

type researchSyncOutcome struct {
	Result   ResearchSyncResult
	Manifest CachedManifest
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

type materializeCachedDocsParams struct {
	CacheRoot string
	CIK       string
	Rows      []FilingRow
	Refresh   bool
	Context   CommandContext
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

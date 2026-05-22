package edgar

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

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

func materializeCachedDocs(ctx context.Context, params materializeCachedDocsParams) (materializedDocs, error) {
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
			content, err := fetchFilingMarkdown(ctx, row, params.Context)
			if err != nil {
				if cliErr, ok := err.(*CLIError); ok && cliErr.Code == ErrorNotFound {
					skipped = append(skipped, map[string]string{"accession": row.Accession, "reason": cliErr.Message})
					continue
				}
				return materializedDocs{}, err
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

func fetchFilingMarkdown(ctx context.Context, row FilingRow, context CommandContext) (string, error) {
	if row.PrimaryDocument == nil || row.FilingURL == nil {
		return "", NewCLIError(ErrorNotFound, "No primary document found for accession "+row.Accession)
	}
	content, err := context.SecClient.FetchSECText(ctx, *row.FilingURL)
	if err != nil {
		return "", err
	}
	return extractMarkdownFromHTML(content), nil
}

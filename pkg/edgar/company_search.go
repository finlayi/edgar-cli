package edgar

import (
	"context"
	"html"
	"regexp"
	"strconv"
	"strings"
)

type CompanySearchParams struct {
	Query string
}

type CompanySearchResult struct {
	CIK        string `json:"cik"`
	CIKNumeric int    `json:"cik_numeric"`
	Name       string `json:"name"`
	State      string `json:"state,omitempty"`
	URL        string `json:"url"`
}

type companySearchPage struct {
	Results   []CompanySearchResult
	Truncated bool
}

var (
	companyPageNamePattern = regexp.MustCompile(`(?is)<span\s+class="companyName">\s*(.*?)\s*<acronym\b[^>]*>\s*CIK\s*</acronym>\s*#:\s*<a\b[^>]*>\s*([0-9]{1,10})`)
	companyStatePattern    = regexp.MustCompile(`(?is)State location:\s*<a\b[^>]*>\s*([^<]+?)\s*</a>`)
	companyRowPattern      = regexp.MustCompile(`(?is)<tr[^>]*>\s*<td[^>]*>\s*<a\b[^>]*CIK=([0-9]{1,10})[^>]*>\s*([0-9]{1,10})\s*</a>\s*</td>\s*<td[^>]*>\s*(.*?)\s*</td>\s*<td[^>]*>\s*(.*?)\s*</td>\s*</tr>`)
	companyNextPagePattern = regexp.MustCompile(`(?is)(rel=["']next["']|value=["']Next\s*\d+["'])`)
	htmlTagPattern         = regexp.MustCompile(`(?is)<[^>]+>`)
	htmlSpacePattern       = regexp.MustCompile(`[ \t\r\n]+`)
)

func runCompanySearch(ctx context.Context, params CompanySearchParams, context CommandContext) (CommandResult, error) {
	query := strings.TrimSpace(params.Query)
	if query == "" {
		return CommandResult{}, NewCLIError(ErrorValidationRequired, "Missing required argument: query")
	}

	content, err := context.SecClient.FetchSECText(ctx, companySearchURL(context.SecClient.Hosts(), query))
	if err != nil {
		return CommandResult{}, err
	}
	page := parseCompanySearchHTML(context.SecClient.Hosts(), content)

	meta := map[string]any{
		"query":                query,
		"query_returned_count": len(page.Results),
		"query_truncated":      page.Truncated,
	}
	if !page.Truncated {
		meta["query_total_count"] = len(page.Results)
	}
	return CommandResult{
		Data:        page.Results,
		MetaUpdates: meta,
	}, nil
}

func parseCompanySearchHTML(hosts SECHosts, content string) companySearchPage {
	if match := companyPageNamePattern.FindStringSubmatch(content); len(match) == 3 {
		result, ok := companySearchResult(hosts, match[2], cleanHTMLText(match[1]), firstCompanyState(content))
		if ok {
			return companySearchPage{Results: []CompanySearchResult{result}}
		}
	}

	results := []CompanySearchResult{}
	seen := map[string]bool{}
	for _, match := range companyRowPattern.FindAllStringSubmatch(content, -1) {
		if len(match) != 5 {
			continue
		}
		result, ok := companySearchResult(hosts, match[1], cleanHTMLText(match[3]), cleanHTMLText(match[4]))
		if !ok || seen[result.CIK] {
			continue
		}
		seen[result.CIK] = true
		results = append(results, result)
	}
	return companySearchPage{
		Results:   results,
		Truncated: companyNextPagePattern.MatchString(content),
	}
}

func companySearchResult(hosts SECHosts, cik string, name string, state string) (CompanySearchResult, bool) {
	normalized, err := normalizeCIK(cik)
	if err != nil {
		return CompanySearchResult{}, false
	}
	numeric, err := strconv.Atoi(normalized)
	if err != nil {
		return CompanySearchResult{}, false
	}
	browseURL, err := companyBrowseURL(hosts, normalized)
	if err != nil {
		return CompanySearchResult{}, false
	}
	return CompanySearchResult{
		CIK:        normalized,
		CIKNumeric: numeric,
		Name:       name,
		State:      state,
		URL:        browseURL,
	}, true
}

func firstCompanyState(content string) string {
	if match := companyStatePattern.FindStringSubmatch(content); len(match) == 2 {
		return cleanHTMLText(match[1])
	}
	return ""
}

func cleanHTMLText(value string) string {
	cleaned := htmlTagPattern.ReplaceAllString(value, " ")
	cleaned = html.UnescapeString(cleaned)
	cleaned = strings.ReplaceAll(cleaned, "\u00a0", " ")
	return strings.TrimSpace(htmlSpacePattern.ReplaceAllString(cleaned, " "))
}

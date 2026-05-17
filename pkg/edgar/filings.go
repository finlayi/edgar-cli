package edgar

import (
	"context"
	"strings"
)

type RecentFilings struct {
	AccessionNumber       []string `json:"accessionNumber"`
	FilingDate            []string `json:"filingDate"`
	ReportDate            []string `json:"reportDate"`
	AcceptanceDateTime    []string `json:"acceptanceDateTime"`
	Act                   []string `json:"act"`
	Form                  []string `json:"form"`
	FileNumber            []string `json:"fileNumber"`
	FilmNumber            []string `json:"filmNumber"`
	Items                 []string `json:"items"`
	Size                  []int    `json:"size"`
	IsXBRL                []int    `json:"isXBRL"`
	IsInlineXBRL          []int    `json:"isInlineXBRL"`
	PrimaryDocument       []string `json:"primaryDocument"`
	PrimaryDocDescription []string `json:"primaryDocDescription"`
}

type SubmissionsPayload struct {
	CIK     string   `json:"cik"`
	Name    string   `json:"name"`
	Tickers []string `json:"tickers"`
	Filings struct {
		Recent RecentFilings `json:"recent"`
	} `json:"filings"`
}

type FilingRow struct {
	Accession       string  `json:"accession"`
	Form            *string `json:"form"`
	FilingDate      *string `json:"filingDate"`
	ReportDate      *string `json:"reportDate"`
	PrimaryDocument *string `json:"primaryDocument"`
	FilingURL       *string `json:"filingUrl"`
}

type FilingsListParams struct {
	ID         string
	Form       string
	From       string
	To         string
	QueryLimit int
	Offset     int
}

type FilingsGetParams struct {
	ID        string
	Accession string
	Format    string
}

func runResolve(ctx context.Context, id string, context CommandContext) (CommandResult, error) {
	entity, err := resolveEntity(ctx, id, context.SecClient, true)
	if err != nil {
		return CommandResult{}, err
	}
	return CommandResult{Data: entity}, nil
}

func zipRecentFilings(hosts SECHosts, cik string, recent RecentFilings) []FilingRow {
	rows := []FilingRow{}
	for idx, accession := range recent.AccessionNumber {
		if accession == "" {
			continue
		}
		row := FilingRow{Accession: accession}
		if idx < len(recent.Form) {
			row.Form = stringPointerOrNil(recent.Form[idx])
		}
		if idx < len(recent.FilingDate) {
			row.FilingDate = stringPointerOrNil(recent.FilingDate[idx])
		}
		if idx < len(recent.ReportDate) {
			row.ReportDate = stringPointerOrNil(recent.ReportDate[idx])
		}
		if idx < len(recent.PrimaryDocument) {
			row.PrimaryDocument = stringPointerOrNil(recent.PrimaryDocument[idx])
		}
		if row.PrimaryDocument != nil {
			if url, err := filingDocumentURL(hosts, cik, accession, *row.PrimaryDocument); err == nil {
				row.FilingURL = &url
			}
		}
		rows = append(rows, row)
	}
	return rows
}

func stringPointerOrNil(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func runFilingsList(ctx context.Context, params FilingsListParams, context CommandContext) (CommandResult, error) {
	entity, err := resolveEntity(ctx, params.ID, context.SecClient, false)
	if err != nil {
		return CommandResult{}, err
	}
	url, err := submissionsURL(context.SecClient.Hosts(), entity.CIK)
	if err != nil {
		return CommandResult{}, err
	}

	var submissions SubmissionsPayload
	if err := context.SecClient.FetchSECJSON(ctx, url, &submissions); err != nil {
		return CommandResult{}, err
	}
	rows := zipRecentFilings(context.SecClient.Hosts(), entity.CIK, submissions.Filings.Recent)

	normalizedForm := strings.ToUpper(strings.TrimSpace(params.Form))
	filtered := []FilingRow{}
	for _, row := range rows {
		if normalizedForm != "" {
			rowForm := ""
			if row.Form != nil {
				rowForm = strings.ToUpper(*row.Form)
			}
			if rowForm != normalizedForm {
				continue
			}
		}
		if row.FilingDate == nil {
			if params.From != "" || params.To != "" {
				continue
			}
		} else if !dateInRange(*row.FilingDate, params.From, params.To) {
			continue
		}
		filtered = append(filtered, row)
	}

	offset := params.Offset
	if offset > len(filtered) {
		offset = len(filtered)
	}
	queryLimit := params.QueryLimit
	if queryLimit == 0 {
		queryLimit = len(filtered)
	}
	end := offset + queryLimit
	if end > len(filtered) {
		end = len(filtered)
	}
	pagedRows := filtered[offset:end]

	return CommandResult{
		Data: pagedRows,
		MetaUpdates: map[string]any{
			"query_total_count":    len(filtered),
			"query_returned_count": len(pagedRows),
			"query_truncated":      offset+len(pagedRows) < len(filtered),
			"query_offset":         params.Offset,
		},
	}, nil
}

func runFilingsGet(ctx context.Context, params FilingsGetParams, context CommandContext) (CommandResult, error) {
	accession, err := normalizeAccession(params.Accession)
	if err != nil {
		return CommandResult{}, err
	}
	entity, err := resolveEntity(ctx, params.ID, context.SecClient, false)
	if err != nil {
		return CommandResult{}, err
	}
	url, err := submissionsURL(context.SecClient.Hosts(), entity.CIK)
	if err != nil {
		return CommandResult{}, err
	}

	var submissions SubmissionsPayload
	if err := context.SecClient.FetchSECJSON(ctx, url, &submissions); err != nil {
		return CommandResult{}, err
	}
	rows := zipRecentFilings(context.SecClient.Hosts(), entity.CIK, submissions.Filings.Recent)
	var match *FilingRow
	for idx := range rows {
		if rows[idx].Accession == accession {
			match = &rows[idx]
			break
		}
	}
	if match == nil {
		return CommandResult{}, NewCLIError(ErrorNotFound, "Accession "+accession+" not found in recent submissions for "+params.ID)
	}
	if match.PrimaryDocument == nil || match.FilingURL == nil {
		return CommandResult{}, NewCLIError(ErrorNotFound, "No primary document found for accession "+accession)
	}

	if params.Format == "url" {
		return CommandResult{Data: map[string]any{
			"accession":       match.Accession,
			"form":            match.Form,
			"filingDate":      match.FilingDate,
			"reportDate":      match.ReportDate,
			"primaryDocument": match.PrimaryDocument,
			"url":             match.FilingURL,
		}}, nil
	}

	content, err := context.SecClient.FetchSECText(ctx, *match.FilingURL)
	if err != nil {
		return CommandResult{}, err
	}
	if params.Format == "html" {
		return CommandResult{Data: map[string]any{
			"accession": match.Accession,
			"url":       match.FilingURL,
			"content":   content,
		}}, nil
	}
	if params.Format == "text" {
		return CommandResult{Data: map[string]any{
			"accession": match.Accession,
			"url":       match.FilingURL,
			"content":   extractPlainTextFromHTML(content),
		}}, nil
	}
	return CommandResult{Data: map[string]any{
		"accession": match.Accession,
		"url":       match.FilingURL,
		"content":   extractMarkdownFromHTML(content),
	}}, nil
}

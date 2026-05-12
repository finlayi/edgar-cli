package edgar

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"
)

type TickerRecord struct {
	CIKStr int    `json:"cik_str"`
	Ticker string `json:"ticker"`
	Title  string `json:"title"`
}

type ResolvedEntity struct {
	Input      string  `json:"input"`
	CIK        string  `json:"cik"`
	CIKNumeric int     `json:"cik_numeric"`
	Ticker     *string `json:"ticker"`
	Title      *string `json:"title"`
}

type tickerMapCache struct {
	mu        sync.Mutex
	records   []TickerRecord
	cachedAt  time.Time
	cacheHost string
}

var globalTickerCache tickerMapCache

const tickerMapTTL = 15 * time.Minute

func getTickerMap(ctx context.Context, client *SecClient) ([]TickerRecord, error) {
	globalTickerCache.mu.Lock()
	if len(globalTickerCache.records) > 0 &&
		time.Since(globalTickerCache.cachedAt) < tickerMapTTL &&
		globalTickerCache.cacheHost == client.Hosts().WWW {
		records := append([]TickerRecord(nil), globalTickerCache.records...)
		globalTickerCache.mu.Unlock()
		return records, nil
	}
	globalTickerCache.mu.Unlock()

	var payload map[string]TickerRecord
	if err := client.FetchSECJSON(ctx, tickerMapURL(client.Hosts()), &payload); err != nil {
		return nil, err
	}
	records := make([]TickerRecord, 0, len(payload))
	for _, record := range payload {
		if record.CIKStr != 0 && record.Ticker != "" {
			records = append(records, record)
		}
	}

	globalTickerCache.mu.Lock()
	globalTickerCache.records = append([]TickerRecord(nil), records...)
	globalTickerCache.cachedAt = time.Now()
	globalTickerCache.cacheHost = client.Hosts().WWW
	globalTickerCache.mu.Unlock()
	return records, nil
}

func resetTickerMapCache() {
	globalTickerCache.mu.Lock()
	defer globalTickerCache.mu.Unlock()
	globalTickerCache.records = nil
	globalTickerCache.cachedAt = time.Time{}
	globalTickerCache.cacheHost = ""
}

func resolveEntity(ctx context.Context, id string, client *SecClient, strictMapMatch bool) (ResolvedEntity, error) {
	records, err := getTickerMap(ctx, client)
	if err != nil {
		return ResolvedEntity{}, err
	}

	if isLikelyCIK(id) {
		cik, err := normalizeCIK(id)
		if err != nil {
			return ResolvedEntity{}, err
		}
		cikNumeric, _ := strconv.Atoi(cik)
		var match *TickerRecord
		for idx := range records {
			if records[idx].CIKStr == cikNumeric {
				match = &records[idx]
				break
			}
		}
		if match == nil && strictMapMatch {
			return ResolvedEntity{}, NewCLIError(ErrorNotFound, "No SEC ticker-map record found for CIK "+cik)
		}
		var ticker *string
		var title *string
		if match != nil {
			ticker = &match.Ticker
			title = &match.Title
		}
		return ResolvedEntity{
			Input:      id,
			CIK:        cik,
			CIKNumeric: cikNumeric,
			Ticker:     ticker,
			Title:      title,
		}, nil
	}

	ticker, err := normalizeTicker(id)
	if err != nil {
		return ResolvedEntity{}, err
	}
	for _, record := range records {
		if strings.ToUpper(record.Ticker) == ticker {
			cik := strconv.Itoa(record.CIKStr)
			return ResolvedEntity{
				Input:      id,
				CIK:        strings.Repeat("0", 10-len(cik)) + cik,
				CIKNumeric: record.CIKStr,
				Ticker:     &record.Ticker,
				Title:      &record.Title,
			}, nil
		}
	}

	return ResolvedEntity{}, NewCLIError(ErrorNotFound, "No SEC ticker-map record found for ticker "+ticker)
}

package edgar

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type cliCapture struct {
	stdout bytes.Buffer
	stderr bytes.Buffer
	code   int
}

func runTestCLI(t *testing.T, args []string, env map[string]string, server *httptest.Server) cliCapture {
	t.Helper()
	resetTickerMapCache()
	var capture cliCapture
	options := []Option{WithIO(&capture.stdout, &capture.stderr), WithEnv(env)}
	if server != nil {
		options = append(options, WithHTTPClient(server.Client()), WithSECHosts(SECHosts{Data: server.URL, WWW: server.URL}))
	}
	capture.code = Run(context.Background(), args, options...)
	return capture
}

func parsePayload(t *testing.T, stdout string) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &payload); err != nil {
		t.Fatalf("invalid json %q: %v", stdout, err)
	}
	return payload
}

func newSECFixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		switch {
		case r.URL.Path == "/files/company_tickers.json":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"0": map[string]any{"cik_str": 320193, "ticker": "AAPL", "title": "Apple Inc."},
			})
		case r.URL.Path == "/submissions/CIK0000320193.json":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"cik": "0000320193",
				"filings": map[string]any{
					"recent": map[string]any{
						"accessionNumber": []string{
							"0000320193-26-000111",
							"0000320193-26-000112",
							"0000320193-25-000079",
							"0000320193-25-000210",
						},
						"form":            []string{"8-K", "10-Q", "10-K", "8-K"},
						"filingDate":      []string{"2026-01-20", "2026-01-30", "2025-10-31", "2025-12-10"},
						"reportDate":      []string{"2026-01-20", "2025-12-27", "2025-09-27", "2025-12-10"},
						"primaryDocument": []string{"aapl-20260120.htm", "aapl-20251227.htm", "aapl-20250927.htm", "aapl-20251110.htm"},
					},
				},
			})
		case r.URL.Path == "/Archives/edgar/data/320193/000032019326000111/aapl-20260120.htm":
			w.Header().Set("content-type", "text/html")
			_, _ = w.Write([]byte("<html><body><h2>Item 5.02</h2><p>Director resigned effective immediately.</p></body></html>"))
		case r.URL.Path == "/Archives/edgar/data/320193/000032019326000112/aapl-20251227.htm":
			w.Header().Set("content-type", "text/html")
			_, _ = w.Write([]byte("<html><body><h2>Item 2</h2><p>Management discussion indicates revenue growth.</p></body></html>"))
		case r.URL.Path == "/Archives/edgar/data/320193/000032019325000079/aapl-20250927.htm":
			w.Header().Set("content-type", "text/html")
			_, _ = w.Write([]byte("<html><body><h2>Item 1A Risk Factors</h2><p>Supply chain and macroeconomic risks.</p></body></html>"))
		case r.URL.Path == "/Archives/edgar/data/320193/000032019325000210/aapl-20251110.htm":
			w.Header().Set("content-type", "text/html")
			_, _ = w.Write([]byte("<html><body><h2>Item 8.01</h2><p>Product launch event update.</p></body></html>"))
		case r.URL.Path == "/api/xbrl/companyfacts/CIK0000320193.json":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"cik":        320193,
				"entityName": "Apple Inc.",
				"facts": map[string]any{
					"us-gaap": map[string]any{
						"Revenues": map[string]any{
							"label": "Revenues",
							"units": map[string]any{
								"USD": []map[string]any{
									{"filed": "2025-10-31", "end": "2025-09-27", "val": 100},
									{"filed": "2026-01-30", "end": "2025-12-27", "val": 120},
								},
							},
						},
					},
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{}`))
		}
	}))
}

func TestCLIHelp(t *testing.T) {
	capture := runTestCLI(t, []string{"--help"}, map[string]string{}, nil)
	if capture.code != 0 {
		t.Fatalf("exit = %d, want 0", capture.code)
	}
	if !strings.Contains(capture.stdout.String(), "Usage: edgar") {
		t.Fatalf("help missing usage: %s", capture.stdout.String())
	}
}

func TestCLIMissingIdentity(t *testing.T) {
	capture := runTestCLI(t, []string{"resolve", "AAPL"}, map[string]string{}, nil)
	if capture.code != 3 {
		t.Fatalf("exit = %d, want 3", capture.code)
	}
	payload := parsePayload(t, capture.stdout.String())
	errPayload := payload["error"].(map[string]any)
	if errPayload["code"] != string(ErrorIdentityRequired) {
		t.Fatalf("error = %#v", errPayload)
	}
}

func TestCLIResolve(t *testing.T) {
	server := newSECFixtureServer(t)
	defer server.Close()

	capture := runTestCLI(t, []string{"--user-agent", "Name user@example.com", "resolve", "AAPL"}, map[string]string{}, server)
	if capture.code != 0 {
		t.Fatalf("exit = %d stderr=%s stdout=%s", capture.code, capture.stderr.String(), capture.stdout.String())
	}
	payload := parsePayload(t, capture.stdout.String())
	if payload["command"] != "resolve" || payload["provider"] != "sec" {
		t.Fatalf("payload = %#v", payload)
	}
	data := payload["data"].(map[string]any)
	if data["cik"] != "0000320193" {
		t.Fatalf("data = %#v", data)
	}
}

func TestCLIFilingsListAndGet(t *testing.T) {
	server := newSECFixtureServer(t)
	defer server.Close()

	list := runTestCLI(t, []string{
		"--user-agent", "Name user@example.com",
		"filings", "list", "--id", "AAPL", "--form", "10-K", "--query-limit", "5",
	}, map[string]string{}, server)
	if list.code != 0 {
		t.Fatalf("list exit = %d stdout=%s stderr=%s", list.code, list.stdout.String(), list.stderr.String())
	}
	listPayload := parsePayload(t, list.stdout.String())
	meta := listPayload["meta"].(map[string]any)
	if meta["query_total_count"].(float64) != 1 || meta["query_returned_count"].(float64) != 1 {
		t.Fatalf("meta = %#v", meta)
	}

	getURL := runTestCLI(t, []string{
		"--user-agent", "Name user@example.com",
		"filings", "get", "--id", "AAPL", "--accession", "0000320193-26-000112", "--format", "url",
	}, map[string]string{}, server)
	if getURL.code != 0 {
		t.Fatalf("get exit = %d stdout=%s stderr=%s", getURL.code, getURL.stdout.String(), getURL.stderr.String())
	}
	data := parsePayload(t, getURL.stdout.String())["data"].(map[string]any)
	wantURL := server.URL + "/Archives/edgar/data/320193/000032019326000112/aapl-20251227.htm"
	if data["url"] != wantURL {
		t.Fatalf("url = %#v, want %q", data["url"], wantURL)
	}
}

func TestCLIFilingsMarkdown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/files/company_tickers.json":
			_ = json.NewEncoder(w).Encode(map[string]any{"0": map[string]any{"cik_str": 320193, "ticker": "AAPL", "title": "Apple Inc."}})
		case r.URL.Path == "/submissions/CIK0000320193.json":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"cik": "0000320193",
				"filings": map[string]any{"recent": map[string]any{
					"accessionNumber": []string{"0000320193-26-000006"},
					"form":            []string{"10-Q"},
					"filingDate":      []string{"2026-01-30"},
					"reportDate":      []string{"2025-12-27"},
					"primaryDocument": []string{"aapl-20251227.htm"},
				}},
			})
		case r.URL.Path == "/Archives/edgar/data/320193/000032019326000006/aapl-20251227.htm":
			_, _ = w.Write([]byte("<html><body><h2>Highlights</h2><table><thead><tr><th>Name</th><th>Value</th></tr></thead><tbody><tr><td>Revenue</td><td>$1</td></tr></tbody></table></body></html>"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	capture := runTestCLI(t, []string{
		"--user-agent", "Name user@example.com", "filings", "get",
		"--id", "AAPL", "--accession", "0000320193-26-000006", "--format", "markdown",
	}, map[string]string{}, server)
	if capture.code != 0 {
		t.Fatalf("exit = %d stdout=%s stderr=%s", capture.code, capture.stdout.String(), capture.stderr.String())
	}
	data := parsePayload(t, capture.stdout.String())["data"].(map[string]any)
	content := data["content"].(string)
	if !strings.Contains(content, "## Highlights") || !strings.Contains(content, "| Name | Value |") || !strings.Contains(content, "| --- | --- |") {
		t.Fatalf("markdown = %q", content)
	}
}

func TestResearchAskExplicitDocs(t *testing.T) {
	tempDir := t.TempDir()
	docPathA := filepath.Join(tempDir, "nvda-8k.md")
	docPathB := filepath.Join(tempDir, "aapl-10k.md")
	manifestPath := filepath.Join(tempDir, "docs.json")
	mustWrite(t, docPathA, "# Item 5.02\nPersis Drell resigned from the Board effective immediately.\nNo disagreement with company operations.\n")
	mustWrite(t, docPathB, "# Item 7\nManagement discussion includes net sales and gross margin analysis.\nRisk factors are discussed in Item 1A.\n")
	mustWrite(t, manifestPath, `{"docs":["`+docPathB+`"]}`)

	capture := runTestCLI(t, []string{
		"research", "ask", "board resigned effective immediately",
		"--doc", docPathA, "--manifest", manifestPath, "--top-k", "3", "--chunk-lines", "20", "--chunk-overlap", "5",
	}, map[string]string{}, nil)
	if capture.code != 0 {
		t.Fatalf("exit = %d stdout=%s stderr=%s", capture.code, capture.stdout.String(), capture.stderr.String())
	}
	data := parsePayload(t, capture.stdout.String())["data"].(map[string]any)
	if data["backend"] != "lexical" || data["result_count"].(float64) == 0 {
		t.Fatalf("data = %#v", data)
	}
	results := data["results"].([]any)
	first := results[0].(map[string]any)
	if first["path"] != docPathA || !strings.Contains(strings.ToLower(first["excerpt"].(string)), "resigned") {
		t.Fatalf("first result = %#v", first)
	}
}

func TestResearchAskDownranksCoverBoilerplate(t *testing.T) {
	tempDir := t.TempDir()
	docPath := filepath.Join(tempDir, "msft-10q.md")
	mustWrite(t, docPath, strings.Join([]string{
		"For the quarterly period ended December 31, 2025",
		"Securities registered pursuant to Section 12(b) of the Act.",
		"Indicate by check mark whether the registrant has filed all required reports.",
		"| Title of each class | Trading Symbol | Name of exchange |",
		"| --- | --- | --- |",
		"| Common stock | MSFT | Nasdaq |",
		"",
		"Management updated quarterly guidance for cloud gross margin.",
		"The company changed guidance after stronger-than-expected AI demand.",
		"Revenue outlook for the latest quarter increased.",
		"",
	}, "\n"))

	capture := runTestCLI(t, []string{
		"research", "ask", "what changed in the latest quarter guidance",
		"--doc", docPath, "--top-k", "1", "--chunk-lines", "6", "--chunk-overlap", "0",
	}, map[string]string{}, nil)
	if capture.code != 0 {
		t.Fatalf("exit = %d stdout=%s stderr=%s", capture.code, capture.stdout.String(), capture.stderr.String())
	}
	data := parsePayload(t, capture.stdout.String())["data"].(map[string]any)
	results := data["results"].([]any)
	first := results[0].(map[string]any)
	excerpt := strings.ToLower(first["excerpt"].(string))
	if !strings.Contains(excerpt, "guidance") || !strings.Contains(excerpt, "changed") {
		t.Fatalf("excerpt = %q", excerpt)
	}
}

func TestResearchSyncAndAskByID(t *testing.T) {
	server := newSECFixtureServer(t)
	defer server.Close()

	tempDir := t.TempDir()
	sync := runTestCLI(t, []string{
		"--user-agent", "Name user@example.com",
		"research", "sync", "--id", "AAPL", "--profile", "core", "--cache-dir", tempDir,
	}, map[string]string{}, server)
	if sync.code != 0 {
		t.Fatalf("sync exit = %d stdout=%s stderr=%s", sync.code, sync.stdout.String(), sync.stderr.String())
	}
	syncData := parsePayload(t, sync.stdout.String())["data"].(map[string]any)
	if syncData["docs_count"].(float64) != 4 || syncData["fetched_count"].(float64) != 4 {
		t.Fatalf("sync data = %#v", syncData)
	}
	manifestPath := syncData["manifest_path"].(string)
	rawManifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rawManifest), `"profile": "core"`) {
		t.Fatalf("manifest = %s", rawManifest)
	}

	ask := runTestCLI(t, []string{
		"--user-agent", "Name user@example.com",
		"research", "ask", "who resigned effective immediately", "--id", "AAPL",
		"--profile", "core", "--cache-dir", tempDir, "--top-k", "3",
	}, map[string]string{}, server)
	if ask.code != 0 {
		t.Fatalf("ask exit = %d stdout=%s stderr=%s", ask.code, ask.stdout.String(), ask.stderr.String())
	}
	askData := parsePayload(t, ask.stdout.String())["data"].(map[string]any)
	if askData["backend"] != "lexical" || askData["profile"] != "core" || askData["result_count"].(float64) == 0 {
		t.Fatalf("ask data = %#v", askData)
	}
}

func TestResearchAskByIDScoped(t *testing.T) {
	server := newSECFixtureServer(t)
	defer server.Close()

	tempDir := t.TempDir()
	capture := runTestCLI(t, []string{
		"--user-agent", "Name user@example.com",
		"research", "ask", "revenue growth", "--id", "AAPL", "--form", "10-Q", "--latest", "1",
		"--cache-dir", tempDir, "--top-k", "3",
	}, map[string]string{}, server)
	if capture.code != 0 {
		t.Fatalf("exit = %d stdout=%s stderr=%s", capture.code, capture.stdout.String(), capture.stderr.String())
	}
	data := parsePayload(t, capture.stdout.String())["data"].(map[string]any)
	if data["corpus_docs_count"].(float64) != 1 {
		t.Fatalf("data = %#v", data)
	}
	scope := data["scope"].(map[string]any)
	if scope["form"] != "10-Q" || scope["latest"].(float64) != 1 {
		t.Fatalf("scope = %#v", scope)
	}
}

func TestFactsLatest(t *testing.T) {
	server := newSECFixtureServer(t)
	defer server.Close()

	capture := runTestCLI(t, []string{
		"--user-agent", "Name user@example.com",
		"facts", "get", "--id", "AAPL", "--taxonomy", "us-gaap", "--concept", "Revenues", "--unit", "USD", "--latest",
	}, map[string]string{}, server)
	if capture.code != 0 {
		t.Fatalf("exit = %d stdout=%s stderr=%s", capture.code, capture.stdout.String(), capture.stderr.String())
	}
	data := parsePayload(t, capture.stdout.String())["data"].(map[string]any)
	latest := data["latest"].(map[string]any)
	usd := latest["USD"].(map[string]any)
	if usd["val"].(float64) != 120 || usd["filed"] != "2026-01-30" {
		t.Fatalf("latest = %#v", latest)
	}
}

func mustWrite(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

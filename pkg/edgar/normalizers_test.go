package edgar

import "testing"

func TestNormalizers(t *testing.T) {
	cik, err := normalizeCIK("320193")
	if err != nil {
		t.Fatal(err)
	}
	if cik != "0000320193" {
		t.Fatalf("normalizeCIK mismatch: %q", cik)
	}

	ticker, err := normalizeTicker("aapl")
	if err != nil {
		t.Fatal(err)
	}
	if ticker != "AAPL" {
		t.Fatalf("normalizeTicker mismatch: %q", ticker)
	}

	accession, err := normalizeAccession("0000320193-26-000006")
	if err != nil {
		t.Fatal(err)
	}
	if accession != "0000320193-26-000006" {
		t.Fatalf("normalizeAccession mismatch: %q", accession)
	}
}

func TestFilingDocumentURL(t *testing.T) {
	url, err := filingDocumentURL(defaultSECHosts(), "0000320193", "0000320193-26-000006", "aapl-20251227.htm")
	if err != nil {
		t.Fatal(err)
	}
	want := "https://www.sec.gov/Archives/edgar/data/320193/000032019326000006/aapl-20251227.htm"
	if url != want {
		t.Fatalf("filingDocumentURL = %q, want %q", url, want)
	}
}

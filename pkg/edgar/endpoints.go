package edgar

import "strings"

const (
	defaultSECDataHost = "https://data.sec.gov"
	defaultSECWWWHost  = "https://www.sec.gov"
)

type SECHosts struct {
	Data string
	WWW  string
}

func defaultSECHosts() SECHosts {
	return SECHosts{Data: defaultSECDataHost, WWW: defaultSECWWWHost}
}

func (h SECHosts) withDefaults() SECHosts {
	if strings.TrimSpace(h.Data) == "" {
		h.Data = defaultSECDataHost
	}
	if strings.TrimSpace(h.WWW) == "" {
		h.WWW = defaultSECWWWHost
	}
	h.Data = strings.TrimRight(h.Data, "/")
	h.WWW = strings.TrimRight(h.WWW, "/")
	return h
}

func submissionsURL(hosts SECHosts, cik string) (string, error) {
	cik10, err := normalizeCIK(cik)
	if err != nil {
		return "", err
	}
	return hosts.withDefaults().Data + "/submissions/CIK" + cik10 + ".json", nil
}

func companyFactsURL(hosts SECHosts, cik string) (string, error) {
	cik10, err := normalizeCIK(cik)
	if err != nil {
		return "", err
	}
	return hosts.withDefaults().Data + "/api/xbrl/companyfacts/CIK" + cik10 + ".json", nil
}

func tickerMapURL(hosts SECHosts) string {
	return hosts.withDefaults().WWW + "/files/company_tickers.json"
}

func filingDocumentURL(hosts SECHosts, cik string, accession string, primaryDocument string) (string, error) {
	cikNumeric, err := cikNumericString(cik)
	if err != nil {
		return "", err
	}
	normalizedAccession, err := normalizeAccession(accession)
	if err != nil {
		return "", err
	}
	accessionNoDash := strings.ReplaceAll(normalizedAccession, "-", "")
	return hosts.withDefaults().WWW + "/Archives/edgar/data/" + cikNumeric + "/" + accessionNoDash + "/" + primaryDocument, nil
}

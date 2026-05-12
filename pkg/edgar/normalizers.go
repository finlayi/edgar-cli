package edgar

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var (
	cikPattern       = regexp.MustCompile(`^\d{1,10}$`)
	tickerPattern    = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9.-]{0,14}$`)
	accessionPattern = regexp.MustCompile(`^\d{10}-\d{2}-\d{6}$`)
	datePattern      = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
)

func isLikelyCIK(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return false
	}
	for _, ch := range trimmed {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

func normalizeCIK(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if !cikPattern.MatchString(trimmed) {
		return "", validationError("Invalid CIK: %s", value)
	}
	return fmt.Sprintf("%010s", trimmed), nil
}

func normalizeTicker(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if !tickerPattern.MatchString(trimmed) {
		return "", validationError("Invalid ticker: %s", value)
	}
	return strings.ToUpper(trimmed), nil
}

func normalizeAccession(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if !accessionPattern.MatchString(trimmed) {
		return "", NewCLIError(ErrorValidationRequired, "--accession must match XXXXXXXXXX-XX-XXXXXX")
	}
	return trimmed, nil
}

func dateInRange(value string, from string, to string) bool {
	if from == "" && to == "" {
		return true
	}
	if from != "" && value < from {
		return false
	}
	if to != "" && value > to {
		return false
	}
	return true
}

func cikNumericString(cik string) (string, error) {
	normalized, err := normalizeCIK(cik)
	if err != nil {
		return "", err
	}
	parsed, err := strconv.Atoi(normalized)
	if err != nil {
		return "", validationError("Invalid CIK: %s", cik)
	}
	return strconv.Itoa(parsed), nil
}

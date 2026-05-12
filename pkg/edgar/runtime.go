package edgar

import (
	"os"
	"strconv"
	"strings"
)

type RuntimeOptions struct {
	JSONMode  bool
	HumanMode bool
	View      string
	Fields    []string
	Limit     int
	Verbose   bool
	UserAgent string
}

type RuntimeInput struct {
	JSON      bool
	Human     bool
	View      string
	Fields    string
	Limit     string
	Verbose   bool
	UserAgent string
}

func buildRuntimeOptions(input RuntimeInput, env map[string]string) (RuntimeOptions, error) {
	humanMode := input.Human
	view := "summary"
	if input.View == "full" {
		view = "full"
	}

	fields, err := parseFields(input.Fields)
	if err != nil {
		return RuntimeOptions{}, err
	}

	limit := 0
	if strings.TrimSpace(input.Limit) != "" {
		parsed, err := parsePositiveInt(input.Limit, "--limit")
		if err != nil {
			return RuntimeOptions{}, err
		}
		limit = parsed
	}

	userAgent := strings.TrimSpace(input.UserAgent)
	if userAgent == "" {
		userAgent = strings.TrimSpace(env["EDGAR_USER_AGENT"])
	}

	return RuntimeOptions{
		JSONMode:  !humanMode,
		HumanMode: humanMode,
		View:      view,
		Fields:    fields,
		Limit:     limit,
		Verbose:   input.Verbose,
		UserAgent: userAgent,
	}, nil
}

func requireUserAgent(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed != "" {
		return trimmed, nil
	}
	return "", NewCLIError(ErrorIdentityRequired, `Missing SEC identity. Set --user-agent "Name email@domain.com" or EDGAR_USER_AGENT.`)
}

func parsePositiveInt(value string, argName string) (int, error) {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || parsed < 1 {
		return 0, validationError("%s must be a positive integer", argName)
	}
	return parsed, nil
}

func parseNonNegativeInt(value string, argName string) (int, error) {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || parsed < 0 {
		return 0, validationError("%s must be a non-negative integer", argName)
	}
	return parsed, nil
}

func parseDateString(value string, argName string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if !datePattern.MatchString(trimmed) {
		return "", validationError("%s must use YYYY-MM-DD", argName)
	}
	return trimmed, nil
}

func environMap(environ []string) map[string]string {
	result := make(map[string]string, len(environ))
	for _, entry := range environ {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			result[key] = value
		}
	}
	return result
}

func processEnv() map[string]string {
	return environMap(os.Environ())
}

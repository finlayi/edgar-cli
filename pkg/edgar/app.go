package edgar

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

type CommandContext struct {
	Runtime   RuntimeOptions
	SecClient *SecClient
	Env       map[string]string
}

type App struct {
	stdout   io.Writer
	stderr   io.Writer
	env      map[string]string
	http     *http.Client
	secHosts SECHosts
}

type Option func(*App)

func WithIO(stdout io.Writer, stderr io.Writer) Option {
	return func(app *App) {
		app.stdout = stdout
		app.stderr = stderr
	}
}

func WithEnv(env map[string]string) Option {
	return func(app *App) {
		app.env = env
	}
}

func WithHTTPClient(client *http.Client) Option {
	return func(app *App) {
		app.http = client
	}
}

func WithSECHosts(hosts SECHosts) Option {
	return func(app *App) {
		app.secHosts = hosts
	}
}

func Run(ctx context.Context, argv []string, options ...Option) int {
	app := &App{
		stdout:   os.Stdout,
		stderr:   os.Stderr,
		env:      processEnv(),
		http:     http.DefaultClient,
		secHosts: defaultSECHosts(),
	}
	for _, option := range options {
		option(app)
	}
	return app.run(ctx, argv)
}

func (app *App) run(ctx context.Context, argv []string) int {
	globalInput, remaining, helpRequested, err := parseGlobalOptions(argv)
	if err != nil {
		return app.emitStartupError("unknown", err)
	}
	runtime, err := buildRuntimeOptions(globalInput, app.env)
	if err != nil {
		return app.emitStartupError(commandNameFromArgs(remaining), err)
	}
	if helpRequested || len(remaining) == 0 {
		app.printHelp()
		return 0
	}

	commandName, requiresIdentity, handler, err := app.buildHandler(ctx, remaining)
	if err != nil {
		return app.emitError(commandNameFromArgs(remaining), toCLIError(err), runtime)
	}

	return app.execute(ctx, commandName, runtime, requiresIdentity, handler)
}

func (app *App) execute(
	ctx context.Context,
	command string,
	runtime RuntimeOptions,
	requiresIdentity bool,
	handler func(CommandContext) (CommandResult, error),
) int {
	userAgent := runtime.UserAgent
	if requiresIdentity {
		required, err := requireUserAgent(userAgent)
		if err != nil {
			return app.emitError(command, toCLIError(err), runtime)
		}
		userAgent = required
	} else if strings.TrimSpace(userAgent) == "" {
		userAgent = "edgar-cli local research"
	}

	client := NewSecClient(SecClientOptions{
		UserAgent:  userAgent,
		Verbose:    runtime.Verbose,
		HTTPClient: app.http,
		Hosts:      app.secHosts,
		Logger: func(message string) {
			fmt.Fprintf(app.stderr, "[debug] %s\n", message)
		},
	})

	result, err := handler(CommandContext{Runtime: runtime, SecClient: client, Env: app.env})
	if err != nil {
		return app.emitError(command, toCLIError(err), runtime)
	}
	return app.emitSuccess(command, result, runtime)
}

func (app *App) emitSuccess(command string, result CommandResult, runtime RuntimeOptions) int {
	if runtime.HumanMode {
		if err := writePrettyJSONLine(app.stdout, result.Data); err != nil {
			fmt.Fprintf(app.stderr, "%s\n", err.Error())
			return 10
		}
		return 0
	}

	shaped, err := shapeData(result.Data, runtime.Fields, runtime.Limit)
	if err != nil {
		return app.emitError(command, toCLIError(err), runtime)
	}
	meta := map[string]any{}
	for key, value := range result.MetaUpdates {
		meta[key] = value
	}
	for key, value := range shaped.MetaUpdates {
		meta[key] = value
	}
	if err := writeJSONLine(app.stdout, successEnvelope(command, shaped.Data, runtime.View, meta)); err != nil {
		fmt.Fprintf(app.stderr, "%s\n", err.Error())
		return 10
	}
	return 0
}

func (app *App) emitError(command string, cliErr *CLIError, runtime RuntimeOptions) int {
	if runtime.HumanMode {
		fmt.Fprintf(app.stderr, "%s %s\n", cliErr.Code, cliErr.Message)
		return cliErr.ExitCode()
	}
	if err := writeJSONLine(app.stdout, failureEnvelope(command, cliErr, runtime.View)); err != nil {
		fmt.Fprintf(app.stderr, "%s\n", err.Error())
		return 10
	}
	return cliErr.ExitCode()
}

func (app *App) emitStartupError(command string, err error) int {
	runtime := RuntimeOptions{JSONMode: true, View: "summary"}
	return app.emitError(command, toCLIError(err), runtime)
}

func (app *App) printHelp() {
	fmt.Fprint(app.stdout, `Usage: edgar [options] <command>

Agent-friendly SEC EDGAR CLI

Options:
  --json                  Emit JSON envelope output (default)
  --human                 Emit human-readable output
  --view <summary|full>   Output view mode
  --fields <fields>       Select specific response fields in JSON mode
  --limit <n>             Limit output rows in JSON mode
  --verbose               Enable verbose debug logs
  --user-agent <value>    SEC identity, e.g. "Name email@domain.com"
  -h, --help              Show help

Commands:
  resolve <id>
  filings list --id <id> [--form <form>] [--from <yyyy-mm-dd>] [--to <yyyy-mm-dd>]
  filings get --id <id> --accession <accession> [--format url|html|text|markdown]
  facts get --id <id> [--taxonomy us-gaap|dei] [--concept <concept>] [--unit <unit>] [--latest]
  research sync --id <id> [--profile core|events|financials] [--cache-dir <path>] [--refresh]
  research ask <query> [--id <id>] [--doc <path>] [--manifest <path>]

SEC identity is required for network commands.
Set --user-agent or EDGAR_USER_AGENT.
`)
}

func (app *App) buildHandler(ctx context.Context, args []string) (string, bool, func(CommandContext) (CommandResult, error), error) {
	switch args[0] {
	case "resolve":
		if len(args) < 2 {
			return "resolve", true, nil, NewCLIError(ErrorValidationRequired, "Missing required argument: id")
		}
		id := args[1]
		return "resolve", true, func(context CommandContext) (CommandResult, error) {
			return runResolve(ctx, id, context)
		}, nil
	case "filings":
		return app.buildFilingsHandler(ctx, args[1:])
	case "facts":
		return app.buildFactsHandler(ctx, args[1:])
	case "research":
		return app.buildResearchHandler(ctx, args[1:])
	default:
		return args[0], false, nil, NewCLIError(ErrorValidationRequired, "Unknown command: "+args[0])
	}
}

func (app *App) buildFilingsHandler(ctx context.Context, args []string) (string, bool, func(CommandContext) (CommandResult, error), error) {
	if len(args) == 0 {
		return "filings", true, nil, NewCLIError(ErrorValidationRequired, "Missing filings subcommand")
	}
	switch args[0] {
	case "list":
		options, positionals, err := parseCommandOptions(args[1:], map[string]bool{
			"--id": true, "--form": true, "--from": true, "--to": true, "--query-limit": true, "--offset": true,
		})
		if err != nil {
			return "filings list", true, nil, err
		}
		if len(positionals) > 0 {
			return "filings list", true, nil, NewCLIError(ErrorValidationRequired, "Unexpected argument: "+positionals[0])
		}
		id := firstOption(options, "--id")
		if id == "" {
			return "filings list", true, nil, NewCLIError(ErrorValidationRequired, "Missing required option: --id")
		}
		from := ""
		if value := firstOption(options, "--from"); value != "" {
			parsed, err := parseDateString(value, "--from")
			if err != nil {
				return "filings list", true, nil, err
			}
			from = parsed
		}
		to := ""
		if value := firstOption(options, "--to"); value != "" {
			parsed, err := parseDateString(value, "--to")
			if err != nil {
				return "filings list", true, nil, err
			}
			to = parsed
		}
		queryLimit := 0
		if value := firstOption(options, "--query-limit"); value != "" {
			parsed, err := parsePositiveInt(value, "--query-limit")
			if err != nil {
				return "filings list", true, nil, err
			}
			queryLimit = parsed
		}
		offset := 0
		if value := firstOptionDefault(options, "--offset", "0"); value != "" {
			parsed, err := parseNonNegativeInt(value, "--offset")
			if err != nil {
				return "filings list", true, nil, err
			}
			offset = parsed
		}
		params := FilingsListParams{
			ID: id, Form: firstOption(options, "--form"), From: from, To: to, QueryLimit: queryLimit, Offset: offset,
		}
		return "filings list", true, func(context CommandContext) (CommandResult, error) {
			return runFilingsList(ctx, params, context)
		}, nil
	case "get":
		options, positionals, err := parseCommandOptions(args[1:], map[string]bool{
			"--id": true, "--accession": true, "--format": true,
		})
		if err != nil {
			return "filings get", true, nil, err
		}
		if len(positionals) > 0 {
			return "filings get", true, nil, NewCLIError(ErrorValidationRequired, "Unexpected argument: "+positionals[0])
		}
		id := firstOption(options, "--id")
		accession := firstOption(options, "--accession")
		if id == "" {
			return "filings get", true, nil, NewCLIError(ErrorValidationRequired, "Missing required option: --id")
		}
		if accession == "" {
			return "filings get", true, nil, NewCLIError(ErrorValidationRequired, "Missing required option: --accession")
		}
		format := firstOptionDefault(options, "--format", "url")
		if !stringIn(format, []string{"url", "html", "text", "markdown"}) {
			return "filings get", true, nil, NewCLIError(ErrorValidationRequired, "--format must be one of url|html|text|markdown")
		}
		params := FilingsGetParams{ID: id, Accession: accession, Format: format}
		return "filings get", true, func(context CommandContext) (CommandResult, error) {
			return runFilingsGet(ctx, params, context)
		}, nil
	default:
		return "filings", true, nil, NewCLIError(ErrorValidationRequired, "Unknown filings subcommand: "+args[0])
	}
}

func (app *App) buildFactsHandler(ctx context.Context, args []string) (string, bool, func(CommandContext) (CommandResult, error), error) {
	if len(args) == 0 || args[0] != "get" {
		return "facts", true, nil, NewCLIError(ErrorValidationRequired, "Missing or unknown facts subcommand")
	}
	options, positionals, err := parseCommandOptions(args[1:], map[string]bool{
		"--id": true, "--taxonomy": true, "--concept": true, "--unit": true, "--latest": false,
	})
	if err != nil {
		return "facts get", true, nil, err
	}
	if len(positionals) > 0 {
		return "facts get", true, nil, NewCLIError(ErrorValidationRequired, "Unexpected argument: "+positionals[0])
	}
	id := firstOption(options, "--id")
	if id == "" {
		return "facts get", true, nil, NewCLIError(ErrorValidationRequired, "Missing required option: --id")
	}
	taxonomy := firstOption(options, "--taxonomy")
	if taxonomy != "" && !stringIn(taxonomy, []string{"us-gaap", "dei"}) {
		return "facts get", true, nil, NewCLIError(ErrorValidationRequired, "--taxonomy must be us-gaap or dei")
	}
	concept := firstOption(options, "--concept")
	unit := firstOption(options, "--unit")
	latest := hasOption(options, "--latest")
	if concept == "" && unit != "" {
		return "facts get", true, nil, NewCLIError(ErrorValidationRequired, "--unit requires --concept")
	}
	if concept == "" && latest {
		return "facts get", true, nil, NewCLIError(ErrorValidationRequired, "--latest requires --concept")
	}
	params := FactsGetParams{
		ID:       id,
		Taxonomy: taxonomy,
		Concept:  concept,
		Unit:     unit,
		Latest:   latest,
	}
	return "facts get", true, func(context CommandContext) (CommandResult, error) {
		return runFactsGet(ctx, params, context)
	}, nil
}

func (app *App) buildResearchHandler(ctx context.Context, args []string) (string, bool, func(CommandContext) (CommandResult, error), error) {
	if len(args) == 0 {
		return "research", false, nil, NewCLIError(ErrorValidationRequired, "Missing research subcommand")
	}
	switch args[0] {
	case "sync":
		options, positionals, err := parseCommandOptions(args[1:], map[string]bool{
			"--id": true, "--profile": true, "--cache-dir": true, "--refresh": false,
		})
		if err != nil {
			return "research sync", true, nil, err
		}
		if len(positionals) > 0 {
			return "research sync", true, nil, NewCLIError(ErrorValidationRequired, "Unexpected argument: "+positionals[0])
		}
		id := firstOption(options, "--id")
		if id == "" {
			return "research sync", true, nil, NewCLIError(ErrorValidationRequired, "Missing required option: --id")
		}
		profile, err := parseResearchProfile(firstOptionDefault(options, "--profile", "core"))
		if err != nil {
			return "research sync", true, nil, err
		}
		params := ResearchSyncParams{
			ID:       id,
			Profile:  profile,
			CacheDir: firstOption(options, "--cache-dir"),
			Refresh:  hasOption(options, "--refresh"),
		}
		return "research sync", true, func(context CommandContext) (CommandResult, error) {
			return runResearchSync(ctx, params, context)
		}, nil
	case "ask":
		options, positionals, err := parseCommandOptions(args[1:], map[string]bool{
			"--id": true, "--profile": true, "--form": true, "--latest": true, "--cache-dir": true,
			"--refresh": false, "--doc": true, "--manifest": true, "--top-k": true,
			"--chunk-lines": true, "--chunk-overlap": true,
		})
		if err != nil {
			return "research ask", false, nil, err
		}
		if len(positionals) == 0 {
			return "research ask", false, nil, NewCLIError(ErrorValidationRequired, "Missing required argument: query")
		}
		query := positionals[0]
		topK, err := parsePositiveInt(firstOptionDefault(options, "--top-k", "8"), "--top-k")
		if err != nil {
			return "research ask", false, nil, err
		}
		chunkLines, err := parsePositiveInt(firstOptionDefault(options, "--chunk-lines", "40"), "--chunk-lines")
		if err != nil {
			return "research ask", false, nil, err
		}
		chunkOverlap, err := parseNonNegativeInt(firstOptionDefault(options, "--chunk-overlap", "10"), "--chunk-overlap")
		if err != nil {
			return "research ask", false, nil, err
		}
		latest := 0
		if value := firstOption(options, "--latest"); value != "" {
			latest, err = parsePositiveInt(value, "--latest")
			if err != nil {
				return "research ask", false, nil, err
			}
		}
		id := firstOption(options, "--id")
		if id == "" && (firstOption(options, "--form") != "" || latest > 0) {
			return "research ask", false, nil, NewCLIError(ErrorValidationRequired, "--form and --latest require --id")
		}
		profile, err := parseResearchProfile(firstOptionDefault(options, "--profile", "core"))
		if err != nil {
			return "research ask", false, nil, err
		}
		if id != "" {
			params := ResearchAskByIDParams{
				ID: id, Query: query, Profile: profile,
				Scope:    AskScope{Form: firstOption(options, "--form"), Latest: latest},
				CacheDir: firstOption(options, "--cache-dir"), Refresh: hasOption(options, "--refresh"),
				TopK: topK, ChunkLines: chunkLines, ChunkOverlap: chunkOverlap,
			}
			return "research ask", true, func(context CommandContext) (CommandResult, error) {
				return runResearchAskByID(ctx, params, context)
			}, nil
		}
		params := ResearchAskParams{
			Query: query, Docs: options["--doc"], ManifestPath: firstOption(options, "--manifest"),
			TopK: topK, ChunkLines: chunkLines, ChunkOverlap: chunkOverlap,
		}
		return "research ask", false, func(context CommandContext) (CommandResult, error) {
			return runResearchAsk(ctx, params, context)
		}, nil
	default:
		return "research", false, nil, NewCLIError(ErrorValidationRequired, "Unknown research subcommand: "+args[0])
	}
}

type globalParseResult struct {
	RuntimeInput RuntimeInput
	Remaining    []string
	Help         bool
}

func parseGlobalOptions(args []string) (RuntimeInput, []string, bool, error) {
	input := RuntimeInput{View: "summary"}
	remaining := []string{}

	for idx := 0; idx < len(args); idx++ {
		arg := args[idx]
		name, value, hasInlineValue := strings.Cut(arg, "=")
		switch name {
		case "--json":
			input.JSON = true
		case "--human":
			input.Human = true
		case "--verbose":
			input.Verbose = true
		case "-h", "--help":
			return input, remaining, true, nil
		case "--view", "--fields", "--limit", "--user-agent":
			if !hasInlineValue {
				if idx+1 >= len(args) {
					return input, remaining, false, NewCLIError(ErrorValidationRequired, "Missing value for "+name)
				}
				idx++
				value = args[idx]
			}
			switch name {
			case "--view":
				input.View = value
			case "--fields":
				input.Fields = value
			case "--limit":
				input.Limit = value
			case "--user-agent":
				input.UserAgent = value
			}
		default:
			remaining = append(remaining, arg)
		}
	}
	return input, remaining, false, nil
}

func parseCommandOptions(args []string, optionTakesValue map[string]bool) (map[string][]string, []string, error) {
	options := map[string][]string{}
	positionals := []string{}
	for idx := 0; idx < len(args); idx++ {
		arg := args[idx]
		if !strings.HasPrefix(arg, "--") {
			positionals = append(positionals, arg)
			continue
		}
		name, value, hasInlineValue := strings.Cut(arg, "=")
		takesValue, known := optionTakesValue[name]
		if !known {
			return nil, nil, NewCLIError(ErrorValidationRequired, "Unknown option: "+name)
		}
		if takesValue {
			if !hasInlineValue {
				if idx+1 >= len(args) {
					return nil, nil, NewCLIError(ErrorValidationRequired, "Missing value for "+name)
				}
				idx++
				value = args[idx]
			}
			options[name] = append(options[name], value)
			continue
		}
		if hasInlineValue {
			return nil, nil, NewCLIError(ErrorValidationRequired, "Option does not take a value: "+name)
		}
		options[name] = append(options[name], "true")
	}
	return options, positionals, nil
}

func firstOption(options map[string][]string, name string) string {
	values := options[name]
	if len(values) == 0 {
		return ""
	}
	return values[len(values)-1]
}

func firstOptionDefault(options map[string][]string, name string, defaultValue string) string {
	if value := firstOption(options, name); value != "" {
		return value
	}
	return defaultValue
}

func hasOption(options map[string][]string, name string) bool {
	return len(options[name]) > 0
}

func stringIn(value string, values []string) bool {
	for _, candidate := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

func commandNameFromArgs(args []string) string {
	if len(args) == 0 {
		return "unknown"
	}
	if len(args) >= 2 {
		switch args[0] {
		case "filings", "facts", "research":
			return args[0] + " " + args[1]
		}
	}
	return args[0]
}

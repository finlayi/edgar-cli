# edgar-cli

Agent-friendly SEC EDGAR CLI for filings and company facts.

## Features

- `npx`-friendly Node/TypeScript package (no Python runtime needed)
- JSON envelope output by default for stable automation
- Strict SEC identity enforcement (`--user-agent` or `EDGAR_USER_AGENT`)
- Core commands:
  - `resolve`
  - `filings list`
  - `filings get`
  - `facts get`
  - `research sync`
  - `research ask`

## Install / Run

```bash
npx edgar-cli --help
```

Local development:

```bash
npm install
npm run build
node dist/cli.js --help
```

## SEC Identity Requirement

SEC endpoints require declared automated access identity.

Use either:

```bash
export EDGAR_USER_AGENT="Your Name your.email@example.com"
```

Or pass per command:

```bash
npx edgar-cli --user-agent "Your Name your.email@example.com" resolve AAPL
```

If identity is missing, commands fail with `IDENTITY_REQUIRED`.

## Examples

```bash
# Resolve ticker -> canonical SEC identity mapping
npx edgar-cli --user-agent "Your Name your.email@example.com" resolve AAPL

# List recent 10-K filings
npx edgar-cli --user-agent "Your Name your.email@example.com" filings list --id AAPL --form 10-K --query-limit 5

# Get filing document URL by accession
npx edgar-cli --user-agent "Your Name your.email@example.com" filings get --id AAPL --accession 0000320193-26-000006 --format url

# Get filing converted to Markdown
npx edgar-cli --user-agent "Your Name your.email@example.com" filings get --id AAPL --accession 0000320193-26-000006 --format markdown

# Get concept data (latest per unit)
npx edgar-cli --user-agent "Your Name your.email@example.com" facts get --id AAPL --taxonomy us-gaap --concept Revenues --latest

# Query explicit local docs (repeat --doc or pass --manifest)
npx edgar-cli research ask "board resignation details" --doc ./cache/nvda-8k.md --top-k 5

# Build a deterministic cached corpus for a ticker/profile
npx edgar-cli --user-agent "Your Name your.email@example.com" research sync --id NVDA --profile core

# Query by ticker against cached corpus (auto-syncs on cache miss)
npx edgar-cli --user-agent "Your Name your.email@example.com" research ask "what changed on the board?" --id NVDA --profile core
```

## Research Profiles and Cache

`research sync` and `research ask --id` use deterministic filing profiles:

- `core`: latest 1x `10-K`, latest 3x `10-Q`, and recent `8-K` (last 180 days, up to 12)
- `events`: recent `8-K` (last 365 days, up to 24)
- `financials`: latest 2x `10-K` and latest 6x `10-Q`

By default, cached corpora are stored in:

- `$EDGAR_CACHE_DIR` (if set), else
- `$XDG_CACHE_HOME/edgar-cli` (if set), else
- `~/.cache/edgar-cli`

Override per command with `--cache-dir`.

## Output Contract (default)

All JSON-mode commands emit:

```json
{
  "ok": true,
  "command": "resolve",
  "provider": "sec",
  "data": {},
  "error": null,
  "meta": {
    "timestamp": "2026-02-11T00:00:00Z",
    "output_schema": "v1",
    "view": "summary"
  }
}
```

## Compliance Notes

- This CLI targets SEC-hosted endpoints only in V0.
- Respect SEC fair-access guidance and use a valid identity in your user-agent.

References:

- [SEC Developer](https://www.sec.gov/developer)
- [SEC Webmaster FAQ: code support](https://www.sec.gov/os/webmaster-faq#code-support)

## Security

See [`SECURITY.md`](SECURITY.md) for vulnerability reporting guidance.

## Development

```bash
npm run typecheck
npm run test
npm run build
```

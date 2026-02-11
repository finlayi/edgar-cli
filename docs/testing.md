# Testing

Run the full test suite:

```bash
npm test
```

Run type checks:

```bash
npm run typecheck
```

Run build:

```bash
npm run build
```

## Current test focus

- JSON envelope contract stability
- Identifier normalization and accession validation
- Filing URL construction and submission filtering
- SEC error mapping (including 403 undeclared automation and 429 retry)
- Facts command filtering and `--latest` behavior

# Testing

Run the full test suite:

```bash
go test ./...
npm --prefix npm test
```

Build the native binary:

```bash
node npm/scripts/build-native.cjs
```

Smoke-test the built runtime:

```bash
./dist/edgar --help
```

## Current test focus

- JSON envelope contract stability
- Identifier normalization and accession validation
- Filing URL construction and submission filtering
- SEC error mapping (including 403 undeclared automation and 429 retry)
- Facts command filtering and `--latest` behavior
- Native npm launcher platform resolution

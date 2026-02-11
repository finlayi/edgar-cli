# Releasing edgar-cli

This project publishes one npm package for `npx` workflows:

1. Wrapper/runtime package: `edgar-cli`

## Prerequisites

1. npm token in GitHub Actions secret `NPM_TOKEN` or npm trusted publishing configured.
2. `package.json` version bumped for the intended release.
3. Branch protection enabled on `main` (PRs + required CI checks).
4. Protected tag pattern `v*` enabled for maintainers/admins.

## Local release checks

```bash
npm ci
npm run lint
npm run typecheck
npm test
npm run build
```

## Release flow

This repository supports both automated and manual release flows.

### Automated release (recommended)

On merge/push to `main`, the `Release On Main` workflow runs after `CI` succeeds:

1. Reads version from `package.json`.
2. Creates/pushes `v<version>` tag if it does not already exist.
3. Creates a GitHub Release for that tag.
4. Dispatches `.github/workflows/release-npm.yml` for publish.

If the tag already exists, it exits without publishing.

### Manual release

1. Bump `package.json` version.
2. Commit + push to `main`.
3. Create and push tag:

```bash
git tag v0.1.0
git push origin v0.1.0
```

4. GitHub Actions runs `.github/workflows/release-npm.yml` and publishes `edgar-cli`.

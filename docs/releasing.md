# Releasing edgar-cli

This project publishes one wrapper npm package plus prebuilt native runtime packages:

1. Wrapper package: `edgar-cli`
2. Native packages:
   - `edgar-cli-darwin-arm64`
   - `edgar-cli-linux-x64`
   - `edgar-cli-win32-x64`

## Prerequisites

1. npm token in GitHub Actions secret `NPM_TOKEN` or npm trusted publishing configured.
2. `npm/package.json` version bumped for the intended release.
3. Platform package versions synced with `npm --prefix npm run sync:versions`.
4. Branch protection enabled on `main` (PRs + required CI checks).
5. Protected tag pattern `v*` enabled for maintainers/admins.

## Local release checks

```bash
go test ./...
npm --prefix npm ci
npm --prefix npm test
node npm/scripts/build-native.cjs
```

## Release flow

This repository supports both automated and manual release flows.

### Automated release (recommended)

On merge/push to `main`, the `Release On Main` workflow runs after `CI` succeeds:

1. Reads version from `npm/package.json`.
2. Creates/pushes `v<version>` tag if it does not already exist.
3. Creates a GitHub Release for that tag.
4. Dispatches `.github/workflows/release-npm.yml` for native package and wrapper publish.

If the tag already exists, it exits without publishing.

### Manual release

1. Bump `npm/package.json` version.
2. Sync platform versions:

```bash
npm --prefix npm run sync:versions
```

3. Commit + push to `main`.
4. Create and push tag:

```bash
git tag v0.1.0
git push origin v0.1.0
```

5. GitHub Actions runs `.github/workflows/release-npm.yml` and publishes `edgar-cli`.

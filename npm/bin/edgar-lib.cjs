const { spawnSync } = require("node:child_process");
const fs = require("node:fs");
const path = require("node:path");

const NATIVE_TARGETS = {
  "darwin-arm64": {
    packageName: "edgar-cli-darwin-arm64",
    binaryRelPath: "bin/edgar"
  },
  "linux-x64": {
    packageName: "edgar-cli-linux-x64",
    binaryRelPath: "bin/edgar"
  },
  "win32-x64": {
    packageName: "edgar-cli-win32-x64",
    binaryRelPath: "bin/edgar.exe"
  }
};

function targetKey(platform, arch) {
  return `${platform}-${arch}`;
}

function resolveNativeBinary({
  platform = process.platform,
  arch = process.arch,
  requireResolve = require.resolve,
  exists = fs.existsSync,
  baseDir = __dirname
} = {}) {
  const key = targetKey(platform, arch);
  const spec = NATIVE_TARGETS[key];

  if (!spec) {
    return {
      supported: false,
      key,
      packageName: null,
      binaryPath: null
    };
  }

  const localNodeModulesBinary = path.resolve(
    baseDir,
    "..",
    "node_modules",
    spec.packageName,
    spec.binaryRelPath
  );
  if (exists(localNodeModulesBinary)) {
    return {
      supported: true,
      key,
      packageName: spec.packageName,
      binaryPath: localNodeModulesBinary
    };
  }

  try {
    const binaryPath = requireResolve(`${spec.packageName}/${spec.binaryRelPath}`);

    return {
      supported: true,
      key,
      packageName: spec.packageName,
      binaryPath
    };
  } catch (error) {
    if (error && error.code === "MODULE_NOT_FOUND") {
      return {
        supported: true,
        key,
        packageName: spec.packageName,
        binaryPath: null
      };
    }
    throw error;
  }
}

function runEdgar({
  argv,
  env = process.env,
  spawn = spawnSync,
  stderr = process.stderr,
  platform = process.platform,
  arch = process.arch,
  resolveNative = resolveNativeBinary
}) {
  const native = resolveNative({ platform, arch });

  if (!native.supported) {
    stderr.write(`edgar-cli native runtime is not available for ${native.key}.\n`);
    return 1;
  }

  if (!native.binaryPath) {
    stderr.write(`edgar-cli native runtime package missing: ${native.packageName}\n`);
    stderr.write("Reinstall with optional dependencies enabled, then retry `npx edgar-cli --help`.\n");
    return 1;
  }

  const result = spawn(native.binaryPath, argv, { stdio: "inherit", env });
  if (result.error) {
    stderr.write(`edgar-cli native launcher failed: ${result.error.message}\n`);
    return 1;
  }
  if (typeof result.status === "number") {
    return result.status;
  }
  if (result.signal) {
    stderr.write(`edgar-cli native launcher terminated by signal ${result.signal}\n`);
    return 1;
  }
  return 1;
}

module.exports = {
  NATIVE_TARGETS,
  resolveNativeBinary,
  runEdgar
};

const assert = require("node:assert/strict");
const test = require("node:test");

const {
  NATIVE_TARGETS,
  resolveNativeBinary,
  runEdgar
} = require("../bin/edgar-lib.cjs");

test("NATIVE_TARGETS covers supported no-prereq platforms", () => {
  assert.deepEqual(Object.keys(NATIVE_TARGETS).sort(), ["darwin-arm64", "linux-x64"]);
});

test("resolveNativeBinary returns unsupported for unknown platform", () => {
  const native = resolveNativeBinary({ platform: "freebsd", arch: "x64" });
  assert.equal(native.supported, false);
  assert.equal(native.binaryPath, null);
});

test("resolveNativeBinary resolves installed platform package from local node_modules", () => {
  const localPath = "/tmp/edgar/node_modules/edgar-cli-linux-x64/bin/edgar";
  const native = resolveNativeBinary({
    platform: "linux",
    arch: "x64",
    baseDir: "/tmp/edgar/bin",
    requireResolve: () => {
      throw new Error("requireResolve should not run for local fast path");
    },
    exists: (binaryPath) => binaryPath === localPath
  });

  assert.equal(native.supported, true);
  assert.equal(native.packageName, "edgar-cli-linux-x64");
  assert.equal(native.binaryPath, localPath);
});

test("resolveNativeBinary falls back to module resolution for nonstandard layouts", () => {
  const native = resolveNativeBinary({
    platform: "linux",
    arch: "x64",
    baseDir: "/tmp/edgar/bin",
    requireResolve: (id) => {
      assert.equal(id, "edgar-cli-linux-x64/bin/edgar");
      return "/tmp/custom-layout/edgar-cli-linux-x64/bin/edgar";
    },
    exists: () => false
  });

  assert.equal(native.supported, true);
  assert.equal(native.packageName, "edgar-cli-linux-x64");
  assert.equal(native.binaryPath, "/tmp/custom-layout/edgar-cli-linux-x64/bin/edgar");
});

test("runEdgar uses native binary when available", () => {
  const calls = [];
  const code = runEdgar({
    argv: ["--help"],
    env: { ...process.env },
    resolveNative: () => ({
      supported: true,
      packageName: "edgar-cli-linux-x64",
      binaryPath: "/tmp/edgar-cli-linux-x64/bin/edgar"
    }),
    spawn: (command, args) => {
      calls.push({ command, args });
      return { status: 0 };
    }
  });

  assert.equal(code, 0);
  assert.equal(calls.length, 1);
  assert.equal(calls[0].command, "/tmp/edgar-cli-linux-x64/bin/edgar");
});

test("runEdgar errors when supported native package is missing", () => {
  let stderr = "";
  const code = runEdgar({
    argv: ["--help"],
    env: { ...process.env },
    resolveNative: () => ({
      supported: true,
      packageName: "edgar-cli-linux-x64",
      binaryPath: null
    }),
    stderr: { write: (line) => (stderr += line) }
  });

  assert.equal(code, 1);
  assert.match(stderr, /native runtime package missing/);
});

test("runEdgar errors when platform is unsupported", () => {
  let stderr = "";
  const code = runEdgar({
    argv: ["--help"],
    resolveNative: () => ({
      supported: false,
      key: "freebsd-x64",
      packageName: null,
      binaryPath: null
    }),
    stderr: { write: (line) => (stderr += line) }
  });

  assert.equal(code, 1);
  assert.match(stderr, /native runtime is not available/);
});

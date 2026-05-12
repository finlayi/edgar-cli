#!/usr/bin/env node

const { runEdgar } = require("./edgar-lib.cjs");

const exitCode = runEdgar({ argv: process.argv.slice(2) });
process.exit(exitCode);

#!/usr/bin/env node
"use strict";

const fs = require("fs");
const path = require("path");
const { spawn } = require("child_process");

const binaryName = process.platform === "win32" ? "muhiyacode.exe" : "muhiyacode";
const binaryPath = path.join(__dirname, "..", "vendor", binaryName);

if (!fs.existsSync(binaryPath)) {
  console.error("MuhiyaCode is not installed correctly: missing " + binaryName + ".");
  console.error("Reinstall without --ignore-scripts so npm can download the release binary.");
  process.exit(1);
}

const child = spawn(binaryPath, process.argv.slice(2), { stdio: "inherit" });

child.on("error", (error) => {
  console.error("Failed to start MuhiyaCode: " + error.message);
  process.exitCode = 1;
});

child.on("exit", (code, signal) => {
  if (signal) {
    process.kill(process.pid, signal);
    return;
  }
  process.exitCode = code === null ? 1 : code;
});

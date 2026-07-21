#!/usr/bin/env node
"use strict";

const crypto = require("crypto");
const fs = require("fs");
const https = require("https");
const path = require("path");
const { execFileSync } = require("child_process");
const { pipeline } = require("stream/promises");

const REPOSITORY = "muhiyatools/MuhiyaCode";
const PLATFORMS = { darwin: "darwin", linux: "linux", win32: "windows" };
const ARCHITECTURES = { x64: "amd64", arm64: "arm64" };
const packageRoot = path.join(__dirname, "..");
const version = require(path.join(packageRoot, "package.json")).version;
const vendorDir = path.join(packageRoot, "vendor");

function fail(message) {
  console.error("\nMuhiyaCode install failed: " + message);
  console.error(`Install manually from https://github.com/${REPOSITORY}/releases/tag/v${version}`);
  process.exit(1);
}

function download(fromUrl, destination, redirects = 5) {
  return new Promise((resolve, reject) => {
    const request = https.get(fromUrl, { headers: { "User-Agent": "muhiyacode-npm-installer" } });
    request.setTimeout(30_000, () => request.destroy(new Error("download timed out")));
    request.on("error", reject);
    request.on("response", (response) => {
      if (response.statusCode >= 300 && response.statusCode < 400 && response.headers.location) {
        response.resume();
        if (redirects === 0) return reject(new Error("too many redirects"));
        return resolve(download(new URL(response.headers.location, fromUrl), destination, redirects - 1));
      }
      if (response.statusCode !== 200) {
        response.resume();
        return reject(new Error(`HTTP ${response.statusCode} for ${fromUrl}`));
      }
      return resolve(pipeline(response, fs.createWriteStream(destination)));
    });
  });
}

function expectedChecksum(checksumsPath, archiveName) {
  const line = fs.readFileSync(checksumsPath, "utf8").split(/\r?\n/)
    .find((entry) => entry.trim().endsWith(`  ${archiveName}`));
  if (!line || !/^[a-f0-9]{64}  /i.test(line)) {
    throw new Error(`checksum for ${archiveName} is missing or invalid`);
  }
  return line.slice(0, 64).toLowerCase();
}

function actualChecksum(archivePath) {
  const hash = crypto.createHash("sha256");
  hash.update(fs.readFileSync(archivePath));
  return hash.digest("hex");
}

async function downloadReleaseFiles(releaseBase, archiveName, archivePath, checksumsPath) {
  const results = await Promise.allSettled([
    download(`${releaseBase}/${archiveName}`, archivePath),
    download(`${releaseBase}/checksums.txt`, checksumsPath),
  ]);
  const failed = results.find((result) => result.status === "rejected");
  if (failed) throw failed.reason;
}

function removeIfPresent(target) {
  try {
    fs.rmSync(target, { force: true, recursive: true });
  } catch (error) {
    if (error.code !== "ENOENT") throw error;
  }
}

function extractArchive(archivePath, destination, isWindows) {
  fs.mkdirSync(destination, { recursive: true });
  const args = isWindows ? ["-xf", archivePath] : ["-xzf", archivePath];
  execFileSync("tar", args, { cwd: destination, stdio: "inherit" });
}

function platformDetails() {
  const goos = PLATFORMS[process.platform];
  const goarch = ARCHITECTURES[process.arch];
  if (!goos || !goarch) {
    throw new Error(`unsupported platform ${process.platform}/${process.arch}`);
  }
  const isWindows = process.platform === "win32";
  return { goos, goarch, isWindows, binaryName: isWindows ? "muhiyacode.exe" : "muhiyacode" };
}

async function install() {
  const { goos, goarch, isWindows, binaryName } = platformDetails();
  const archiveName = `muhiyacode_${version}_${goos}_${goarch}.${isWindows ? "zip" : "tar.gz"}`;
  const releaseBase = `https://github.com/${REPOSITORY}/releases/download/v${version}`;
  const archivePath = path.join(vendorDir, archiveName);
  const checksumsPath = path.join(vendorDir, "checksums.txt");
  const stagingDir = path.join(vendorDir, `.install-${process.pid}`);

  fs.mkdirSync(vendorDir, { recursive: true });
  removeIfPresent(stagingDir);
  console.log("MuhiyaCode: downloading and verifying " + archiveName + "...");
  try {
    await downloadReleaseFiles(releaseBase, archiveName, archivePath, checksumsPath);
    if (actualChecksum(archivePath) !== expectedChecksum(checksumsPath, archiveName)) {
      throw new Error(`checksum mismatch for ${archiveName}`);
    }
    extractArchive(archivePath, stagingDir, isWindows);
    const stagedBinary = path.join(stagingDir, binaryName);
    if (!fs.existsSync(stagedBinary)) throw new Error(`${binaryName} not found in archive`);
    removeIfPresent(path.join(vendorDir, binaryName));
    fs.renameSync(stagedBinary, path.join(vendorDir, binaryName));
    if (!isWindows) fs.chmodSync(path.join(vendorDir, binaryName), 0o755);
  } finally {
    removeIfPresent(archivePath);
    removeIfPresent(checksumsPath);
    removeIfPresent(stagingDir);
  }
  console.log("MuhiyaCode: installed " + binaryName);
}

if (process.env.MUHIYACODE_SKIP_DOWNLOAD) {
  console.log("MuhiyaCode: MUHIYACODE_SKIP_DOWNLOAD set; skipping binary download.");
} else {
  install().catch((error) => fail(error.message));
}

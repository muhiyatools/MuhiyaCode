#!/usr/bin/env node
// Downloads the prebuilt MuhiyaCode binary that matches this machine from the
// GitHub Release whose tag is "v<package.json version>", then unpacks it next to
// the npm package so bin/muhiyacode.js can launch it. Runs automatically on
// `npm install`. Pure Node built-ins — no dependencies.

const fs = require("fs");
const os = require("os");
const path = require("path");
const https = require("https");
const { execFileSync } = require("child_process");

// --- edit this if your repository owner/name differs -----------------------
const REPO = "muhiya/muhiyacode";
// ---------------------------------------------------------------------------

const pkg = require(path.join(__dirname, "..", "package.json"));
const version = pkg.version; // must equal the GitHub release tag WITHOUT the leading "v"
const vendorDir = path.join(__dirname, "..", "vendor");

// Map Node's platform/arch names to the ones GoReleaser uses in archive names.
const PLATFORMS = { darwin: "darwin", linux: "linux", win32: "windows" };
const ARCHES = { x64: "amd64", arm64: "arm64" };

function fail(msg) {
  console.error("\nMuhiyaCode install failed: " + msg);
  console.error(
    "You can install manually from https://github.com/" + REPO + "/releases\n"
  );
  process.exit(1);
}

const goos = PLATFORMS[process.platform];
const goarch = ARCHES[process.arch];
if (!goos || !goarch) {
  fail("unsupported platform " + process.platform + "/" + process.arch);
}

const isWindows = process.platform === "win32";
const ext = isWindows ? "zip" : "tar.gz";
const binName = isWindows ? "muhiyacode.exe" : "muhiyacode";
const archiveName = `muhiyacode_${version}_${goos}_${goarch}.${ext}`;
const url = `https://github.com/${REPO}/releases/download/v${version}/${archiveName}`;

// Escape hatch: skip the download entirely and reuse a local build.
if (process.env.MUHIYACODE_SKIP_DOWNLOAD) {
  console.log("MuhiyaCode: MUHIYACODE_SKIP_DOWNLOAD set, skipping binary download.");
  process.exit(0);
}

function download(fromUrl, toFile, redirects) {
  return new Promise((resolve, reject) => {
    https
      .get(fromUrl, { headers: { "User-Agent": "muhiyacode-npm-installer" } }, (res) => {
        // GitHub release assets redirect to a storage host — follow it.
        if (res.statusCode >= 300 && res.statusCode < 400 && res.headers.location) {
          if (redirects <= 0) return reject(new Error("too many redirects"));
          res.resume();
          return resolve(download(res.headers.location, toFile, redirects - 1));
        }
        if (res.statusCode !== 200) {
          res.resume();
          return reject(new Error("HTTP " + res.statusCode + " for " + fromUrl));
        }
        const out = fs.createWriteStream(toFile);
        res.pipe(out);
        out.on("finish", () => out.close(resolve));
        out.on("error", reject);
      })
      .on("error", reject);
  });
}

async function main() {
  fs.mkdirSync(vendorDir, { recursive: true });
  const archivePath = path.join(vendorDir, archiveName);

  console.log("MuhiyaCode: downloading " + archiveName + " …");
  await download(url, archivePath, 5);

  // Windows ships bsdtar (`tar`) since Win10 1803 and it extracts BOTH .zip and
  // .tar.gz, so a single `tar` call covers every platform.
  const args = ext === "zip" ? ["-xf", archivePath] : ["-xzf", archivePath];
  execFileSync("tar", args, { cwd: vendorDir, stdio: "inherit" });

  const binPath = path.join(vendorDir, binName);
  if (!fs.existsSync(binPath)) {
    fail("binary " + binName + " not found in archive");
  }
  if (!isWindows) fs.chmodSync(binPath, 0o755);
  try {
    fs.unlinkSync(archivePath);
  } catch {}
  console.log("MuhiyaCode: installed " + binName);
}

main().catch((err) => fail(err.message));

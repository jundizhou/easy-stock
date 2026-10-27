# Desktop automatic updates

The packaged macOS and Windows apps use `electron-updater` with the public GitHub provider for
[`jundizhou/easy-stock`](https://github.com/jundizhou/easy-stock/releases). Checking for updates and downloading packages require no OSS service or client token. Both runtime and packaging use the configuration in `update-feed.cjs`.

- macOS publishes DMGs, ZIPs, blockmaps and `latest-mac.yml` to GitHub Releases. The app currently checks for updates and opens the release page for manual installation; users should choose the DMG for their architecture.
- Windows publishes the NSIS installer, its blockmap and `latest.yml` to the same GitHub Release for both application updates and manual installation.
- The GitHub provider resolves metadata and packages from the selected release tag, so a later release does not change the package URL of an update already being downloaded.
- The app checks 30 seconds after startup and every 12 hours. Downloads and restarts always require a user action.
- Before installation, the app stops its local services (killing the whole child process tree on Windows so the Hermes runtime cannot linger), flushes Electron sessions, and backs up user data outside the Electron `userData` directory. Files that remain locked (antivirus scans, slow-exiting processes) are retried with backoff and, if they never unlock, skipped and recorded under `skipped` in the backup `manifest.json` instead of aborting the update. The latest three backups are retained.
- A persistence migration/open failure prevents the desktop backend from starting instead of silently opening an empty database.
- An explicit `A_STOCK_UPDATE_FEED_URL` selects a custom generic feed; leave it unset to use GitHub. It must use HTTPS except for loopback hosts (`127.0.0.1`, `localhost`, `[::1]`), where plain HTTP is allowed for local update tests.

Release signing secrets used by `.github/workflows/release.yml`:

- macOS: `MAC_CSC_LINK`, `MAC_CSC_KEY_PASSWORD`, `APPLE_ID`, `APPLE_APP_SPECIFIC_PASSWORD`, `APPLE_TEAM_ID`
- Windows: `WIN_CSC_LINK`, `WIN_CSC_KEY_PASSWORD`

Publishing uses the workflow's built-in `github.token` with `contents: write`; no OSS credentials or tools are required.

Without signing credentials, CI can still produce packages for smoke testing, but those packages are not production-ready for unattended replacement. macOS automatic installation requires a Developer ID signature and notarization; Windows should use Authenticode to avoid an untrusted installer/update path.

## Release procedure

Every release must be made by pushing a tag matching `desktop/package.json`, or by manually running the `Desktop Release` workflow with an existing tag. For runtime upgrades, run the workflow with `build_only: true` and the intended version tag first. This checks out the selected workflow commit, builds and verifies all three platforms, and leaves publication disabled even if every check passes. After all builds pass, one GitHub publishing job uploads and verifies both installation and update assets.

The workflow performs these checks:

1. Build and verify macOS arm64, macOS x64 and Windows x64 assets.
2. Merge and verify updater metadata.
3. Stage DMGs, ZIPs, the EXE, blockmaps and both `latest*.yml` files together; generate `SHA256SUMS.txt` for all of them.
4. Upload the complete asset set to a GitHub Release draft; verify every asset's size and GitHub SHA-256 digest before publishing it as the latest release. The draft keeps new update metadata invisible until its packages are available.

If only publishing fails, dispatch the workflow from `main` with the same tag and the original `build_run_id`. Source tests run again, while packaging reuses the original artifacts only after verifying their tag commit, workflow, successful platform checks and unexpired SHA-256 digests. Publishing scripts come from the dispatched workflow revision so upload repairs do not require changing an already published tag.

Keep `latest*.yml`, the packages they reference, and blockmaps attached to each GitHub Release. Older blockmaps can still be used for differential updates. To validate an existing local asset directory and stage it for publication:

```bash
node desktop/scripts/verify-updater-artifacts.mjs desktop/dist/release latest-mac.yml latest.yml
node desktop/scripts/prepare-publish-assets.mjs desktop/dist/release /tmp/easy-stock-publish v0.4.0
node desktop/scripts/verify-updater-artifacts.mjs /tmp/easy-stock-publish/github latest-mac.yml latest.yml
```

## Migrating existing installations

Previously shipped versions still contain the OSS feed URL. Users must install the first GitHub-enabled release manually from GitHub Releases; subsequent updates use GitHub. Reusing old build artifacts does not change their embedded update source, so the first migrated release must be built from the new source.

Removing OSS publication from this repository does not disable existing public objects or change installed clients. Old clients may continue downloading old packages from OSS until they migrate. Retiring those objects is a separate operation that affects their update path. Daily review content synchronization is separate from desktop updates.

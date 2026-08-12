# Desktop automatic updates

The packaged macOS and Windows apps use `electron-updater` with the public OSS update feed:
`https://easy-stock-fs.oss-cn-beijing.aliyuncs.com/updates/desktop`.

- macOS publishes a signed/notarized ZIP, its blockmap and `latest-mac.yml` to OSS for automatic updates; DMGs are published to GitHub Releases for manual installation.
- Windows publishes a signed NSIS installer, its blockmap and `latest.yml` to OSS for automatic updates; the installer is also published to GitHub Releases for manual installation.
- The app checks 30 seconds after startup and every 12 hours. Downloads and restarts always require a user action.
- Before installation, the app stops its local services, flushes Electron sessions, and backs up user data outside the Electron `userData` directory. The latest three backups are retained.
- A persistence migration/open failure prevents the desktop backend from starting instead of silently opening an empty database.

Release signing and OSS secrets expected by `.github/workflows/release.yml`:

- macOS: `MAC_CSC_LINK`, `MAC_CSC_KEY_PASSWORD`, `APPLE_ID`, `APPLE_APP_SPECIFIC_PASSWORD`, `APPLE_TEAM_ID`
- Windows: `WIN_CSC_LINK`, `WIN_CSC_KEY_PASSWORD`
- OSS: `OSS_ACCESS_KEY_ID`, `OSS_ACCESS_KEY_SECRET` (write permission limited to `easy-stock-fs/updates/desktop/*`)

Without signing credentials, CI can still produce packages for smoke testing, but those packages are not production-ready for unattended replacement. macOS automatic installation requires a Developer ID signature and notarization; Windows should use Authenticode to avoid an untrusted installer/update path.

## Release procedure

Every release must be made by pushing a tag matching `desktop/package.json`, or by manually running the `Desktop Release` workflow with an existing tag. The workflow performs the following steps in order:

1. Build and verify macOS arm64, macOS x64 and Windows x64 assets.
2. Merge and verify updater metadata.
3. Upload versioned ZIP/EXE updater assets to OSS with immutable caching.
4. Upload `latest-mac.yml` and `latest.yml` last with `no-cache`, then probe every public URL.
5. Publish only the user-facing DMGs and Windows installer plus `SHA256SUMS.txt` to GitHub Release.

Do not manually delete updater files from OSS. The two `latest*.yml` objects are the update channels, and all files they reference must remain available. To validate an existing local asset directory:

```bash
node desktop/scripts/verify-updater-artifacts.mjs desktop/dist/release latest-mac.yml latest.yml
node desktop/scripts/prepare-publish-assets.mjs desktop/dist/release /tmp/easy-stock-publish v0.4.0
bash desktop/scripts/publish-updater-oss.sh /tmp/easy-stock-publish/updater
```

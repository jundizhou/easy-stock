# Desktop automatic updates

The packaged macOS and Windows apps use `electron-updater` with GitHub Releases.

- macOS publishes a signed/notarized ZIP for automatic updates and a DMG for manual installation.
- Windows publishes a signed NSIS installer plus its blockmap. A portable ZIP remains available for manual use.
- The app checks 30 seconds after startup and every 12 hours. Downloads and restarts always require a user action.
- Before installation, the app stops its local services, flushes Electron sessions, and backs up user data outside the Electron `userData` directory. The latest three backups are retained.
- A persistence migration/open failure prevents the desktop backend from starting instead of silently opening an empty database.

Release signing secrets expected by `.github/workflows/release.yml`:

- macOS: `MAC_CSC_LINK`, `MAC_CSC_KEY_PASSWORD`, `APPLE_ID`, `APPLE_APP_SPECIFIC_PASSWORD`, `APPLE_TEAM_ID`
- Windows: `WIN_CSC_LINK`, `WIN_CSC_KEY_PASSWORD`

Without signing credentials, CI can still produce packages for smoke testing, but those packages are not production-ready for unattended replacement. macOS automatic installation requires a Developer ID signature and notarization; Windows should use Authenticode to avoid an untrusted installer/update path.

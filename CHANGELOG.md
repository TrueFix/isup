# Changelog

## v2.0.0 — 2026-10-16

### Added

- Responsive live terminal dashboard with per-source results, response times,
  recent sample history, keyboard navigation, and compact layouts.
- Automatic, terminal, and headless modes with independently configurable desktop
  notifications and optional verbose source logging.
- Seven built-in checks across Cloudflare, Google, Quad9, Mozilla, and Apple,
  using direct DNS queries and validated HTTPS responses.
- A DEGRADED state for working public DNS with unavailable web connectivity.
- Custom web endpoints and DNS resolvers, configurable deadlines and thresholds.
- Daily user-writable logs, configurable retention, and terminal-only fallback.
- Automated regression tests, race detection, dependency vulnerability scanning,
  and CI coverage on Linux, Windows, and macOS.
- Release archives and SHA-256 checksums for amd64 and arm64 on all three OSes.

### Changed

- Replaced the single-provider WebSocket connection with fresh, concurrent,
  deadline-bound checks of every configured source.
- Internet Down now requires every source to fail for the configured number of
  consecutive samples; isolated provider failures do not trigger outage alerts.
- Headless mode defaults to notifications off; terminal mode defaults to on.
- Notification sounds are controlled by the operating system; the application no
  longer initializes an audio device or plays bundled MP3s.
- Source builds require Go 1.26 or newer for current patched dependencies.

### Fixed

- Indefinitely stalled connectivity checks after silent network loss.
- Channel double-closes, unsafe shared state, and connection lifecycle leaks.
- Shutdown now cancels in-flight network operations without a false DOWN event.
- Linux startup no longer needs write access to `/var/log` or administrator rights.
- Notification failures and slow desktop services no longer block monitoring.

### Migration

- Use `isup -mode=terminal` for the live dashboard or `isup -mode=headless` for
  plain logs. Override either mode with `-notify=true` or `-notify=false`.
- The legacy internal `isup` and `wss` Go packages have been replaced by the
  `monitor` and `terminal` packages. This is an application release, not a
  backwards-compatible library update.
- Logs now live in the current user's cache directory, under `isup`, rather than
  the old Linux system log directory. Use `-log-dir` to choose another location.
- Windows release downloads now target amd64 and arm64; 32-bit x86 packages are
  no longer produced.

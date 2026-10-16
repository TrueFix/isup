# IsUp

A lightweight Internet reachability monitor for Windows, Linux, and macOS.
Includes a live terminal dashboard, plain headless logging, and optional desktop notifications.
No administrator access, audio drivers, or background service installation required.

## How it works

- Checks **seven sources across five providers**, using both direct public DNS
  queries and HTTPS connectivity checks. Every source has an independent result.
- Validates HTTPS certificates, does not follow redirects, and requires either
  HTTP **204** or the provider's exact expected success content. Captive-portal
  login pages and unexpected content count as failures.
- Starts in **UNKNOWN**. By default, two consecutive successful samples establish
  **UP**. Three consecutive samples where **every source fails** establish
  **DOWN**. An initially offline connection generates a DOWN notification too.
- If public DNS responds but all web checks fail, reports **DEGRADED** after three
  consecutive samples and sends a distinct Limited Connectivity notification,
  rather than claiming the entire Internet connection is down.
- Samples every 5 seconds, with a 4-second deadline for the entire parallel check.
  Checks never overlap. All seven probes run to completion or the shared deadline
  so the dashboard can display real results for every source. Shutdown cancels
  active checks. Connections are freshly established for each sample.
- Notifies only on confirmed state changes, avoiding repeated outage alerts.

These checks measure reachability to the configured endpoints, not every website
or application. Firewalls, VPNs, proxies, or provider blocking can affect results.
Configure endpoints appropriate for your network if necessary. Detection is
sampled, so reported state durations are approximate, not exact outage timings.

### Default sources

| Provider | Target | What must succeed |
| --- | --- | --- |
| Cloudflare DNS | `1.1.1.1:53` | Valid DNS A response for `example.com` |
| Google DNS | `8.8.8.8:53` | Valid DNS A response for `example.com` |
| Quad9 DNS | `9.9.9.9:53` | Valid DNS A response for `example.com` |
| Cloudflare Web | `https://cp.cloudflare.com/generate_204` | HTTP 204 |
| Google Web | `https://www.gstatic.com/generate_204` | HTTP 204 |
| Mozilla Web | `https://detectportal.firefox.com/success.txt` | HTTP 200 + exact success text |
| Apple Web | `https://www.apple.com/library/test/success.html` | HTTP 200 + exact success page |

DNS checks send real queries directly to the listed IPs, validate the reply and
transaction ID, and require a public IPv4 answer. They bypass the hosts file and
the system resolver. Truncated UDP replies fall back to TCP. They do not require
ICMP/ping privileges. Networks that block external DNS can still be UP when an
HTTPS check succeeds; a single blocked provider never causes an outage alert.

**UP** means at least one web check succeeded. **DEGRADED** means DNS responds
but all web checks failed. **DOWN** requires every configured check to fail for
the full failure threshold. No finite set of probes can prove every Internet
service is unavailable, but an Internet Down alert is never based on one failed
provider or only on blocked public DNS.

## Install

Download the archive matching your OS and CPU from
[Releases](https://github.com/TrueFix/isup/releases), extract it, and run `isup`
(`isup.exe` on Windows). `amd64` is for Intel/AMD 64-bit machines; `arm64` is for
Apple Silicon and other ARM64 machines. `darwin` means macOS.

Release archives include this README, the changelog, the license, and the
executable. Each has a SHA-256 checksum sidecar. On Linux, for example:

```sh
sha256sum -c isup-VERSION-linux-amd64.tar.gz.sha256
tar -xzf isup-VERSION-linux-amd64.tar.gz
./isup
```

On macOS, verify with `shasum -a 256 ARCHIVE` and compare with the checksum file.
On Windows, use `Get-FileHash ARCHIVE -Algorithm SHA256` in PowerShell.
Release builds are currently unsigned and macOS builds are not notarized.

### Build from source

Requires Go 1.26 or newer (including current patched networking dependencies).
No C compiler is required. Downloaded binaries do not require Go to be installed.

```sh
git clone https://github.com/TrueFix/isup.git
cd isup
go build -trimpath -o isup .
./isup
```

On Windows, use `go build -trimpath -o isup.exe .` and `./isup.exe`.
The Go module path retains its original name for compatibility with the project
history; the source repository is `TrueFix/isup`.

## Usage

```sh
isup                               # auto: dashboard in a TTY, plain logs otherwise
isup -mode=terminal                # live dashboard + desktop notifications
isup -mode=terminal -notify=false  # live dashboard without notifications
isup -mode=headless                # plain logs, no desktop notifications
isup -mode=headless -notify=true   # plain logs + desktop notifications
isup -mode=headless -verbose       # include every source's result in the logs
isup -interval=10s -timeout=3s      # reduce check frequency
isup -failures=2 -successes=1       # faster transitions, less noise filtering
isup -log-dir=/path/to/user/logs    # explicit writable directory
isup -version
isup -help
```

Stop with **Ctrl+C** (or SIGTERM on Unix). In-flight checks are canceled, logs are
closed, and stopping does not generate an Internet Down event.

| Flag | Default | Meaning |
| --- | --- | --- |
| `-mode` | `auto` | `terminal` dashboard, `headless` logs, or automatic TTY detection |
| `-interval` | `5s` | Time between samples; checks never overlap |
| `-timeout` | `4s` | Maximum duration of one parallel check |
| `-failures` | `3` | Consecutive failed samples required for DOWN |
| `-successes` | `2` | Consecutive successful samples required for UP |
| `-notify` | On for terminal; off for headless | Independently enable/disable desktop notifications |
| `-verbose` | `false` | Log sample summaries and all source results |
| `-endpoint` | Seven built-in sources | Repeatable HTTP(S) URL returning 204; replaces defaults |
| `-dns` | Seven built-in sources | Repeatable resolver IP or IP:port; replaces defaults |
| `-log-dir` | User cache directory + `isup` | Directory for logs and notification icons |
| `-log-retention` | `30` | Days of dated logs to retain |
| `-version` | | Print version and exit |

Durations accept Go syntax such as `500ms`, `5s`, or `1m`. Durations, thresholds,
and retention must be positive. Up to 16 endpoints can be configured.

### Live terminal dashboard

The full-screen dashboard adapts to terminal resizing and includes:

- Confirmed overall state, response count, and pending transition thresholds.
- Live per-source checking indicators, response times, and failure details.
- Recent sample history, online-sample percentage, and time in the current state.
- A source list that scrolls to keep the selected source visible on short screens.
- Compact layouts for narrow windows, plus a recent-activity line when space allows.

Use **↑/↓** or **j/k** to select a source and inspect its address/error, **Home/End**
or **Page Up/Page Down** to navigate, and **q**, **Esc**, or **Ctrl+C** to quit.
Response times include the entire DNS query or web request (DNS/TLS/HTTP), not
ICMP ping time. Online percentage is based on completed samples, not elapsed time.

Auto mode uses the dashboard only when both stdin and stdout are interactive
terminals and `TERM` is not `dumb`. Pipes and services automatically use plain
logs without ANSI screen control. Explicit terminal mode requires a TTY.

### Custom endpoints

Use endpoints you control that return **204 No Content**. At least one responding
correctly counts as online. Providing either `-endpoint` or `-dns` replaces the
entire built-in source list; repeat the flags to build your own list.

```sh
isup -endpoint=https://one.example.net/connectivity -endpoint=https://two.example.net/connectivity
isup -endpoint=https://one.example.net/connectivity -dns=1.1.1.1 -dns=8.8.8.8
isup -mode=headless -dns=1.1.1.1 -dns=9.9.9.9
```

An endpoint returning 200 does **not** count as online. HTTP is allowed for local
networks, but HTTPS is recommended to distinguish the real endpoint from a captive
portal. Standard `HTTP_PROXY`, `HTTPS_PROXY`, and `NO_PROXY` settings are honored;
TLS verification is always enabled.

For a DNS-only custom configuration, successful DNS establishes UP; this tests
DNS reachability only and does not verify web access. Public DNS queries are
direct and are not sent through HTTP proxies.

### Notifications

Desktop notifications require a working desktop notification service. Linux
typically needs a D-Bus desktop session; macOS and Windows may require permission
in system notification settings. Notification sounds depend on the operating
system's settings; IsUp no longer initializes an audio device or plays bundled MP3s.
Headless mode defaults to notifications off. Use `-notify=true` to enable them
when a desktop service is available; `-notify=false` disables them in any mode.

Notification errors are logged and do not stop monitoring. Notifications run
separately from checks, with a bounded queue that keeps the latest pending state.
A slow desktop service cannot block checking or shutdown.

### Logs

Logs are printed as plain text in headless mode and appended to
`isup-YYYY-MM-DD.log` under:

- **Linux:** `$XDG_CACHE_HOME/isup`, normally `~/.cache/isup`
- **macOS:** `~/Library/Caches/isup`
- **Windows:** `%LocalAppData%\isup`

Logs rotate when the local date changes, and expired dated logs are cleaned up
at startup and rotation. Other files are preserved. If file logging fails,
monitoring continues with terminal output and an error on stderr.

State changes include the previous confirmed state's duration. Failed transitions
also include endpoint errors for diagnosis. Logs do not record every successful
sample unless `-verbose` is enabled. In dashboard mode, logs go to the file and
the recent-activity display rather than corrupting the live screen. There is no
telemetry or account; each sample sends requests to the configured providers,
which can see your source IP address.

## Development and releases

```sh
go test -race -cover ./...
go vet ./...
go mod verify
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
```

Tests use local HTTP servers rather than the public Internet. They cover stalled
connections, deadlines, cancellation, multi-provider consensus, direct DNS replies,
captive portals, TLS validation, partial-connectivity transitions, log retention,
process shutdown, terminal resizing, keyboard navigation, and safe error rendering.

CI tests Linux, Windows, and macOS. Pushing a semantic version tag such as
`v2.0.1` triggers tests and dependency checks, then builds CGO-free `amd64` and
`arm64` archives for all three platforms. GitHub Actions creates the release and
uploads archives and checksums using its built-in repository token. Existing
manually published releases are also supported. Build versions come from the tag.

Maintainers can publish entirely through the SSH Git remote. Replace `vX.Y.Z`
with the next unused semantic version:

```sh
git push origin master
git tag -a vX.Y.Z -m "IsUp vX.Y.Z"
git push origin vX.Y.Z
```

The release is available after the Release workflow finishes successfully. No
local GitHub CLI login or custom token secret is required. Prerelease tags such
as `v2.1.0-rc.1` are published as prereleases.

## License

See [LICENSE](LICENSE).

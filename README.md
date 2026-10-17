# 🌐 IsUp

**Know when your internet is really down** — not just one site or DNS server flaking.

Lightweight monitor for **Windows**, **Linux**, and **macOS**. Live terminal dashboard 📊, headless logging ⚙️, optional desktop alerts 🔔. No admin rights, no audio drivers, no background service install.

---

## ✨ Highlights

- **7 checks** across **5 providers** (DNS + HTTPS)
- **Smart states:** UP · DEGRADED · DOWN — fewer false alarms
- **Terminal dashboard** with per-source latency and history
- **Headless mode** for scripts, pipes, and systemd/Task Scheduler
- Logs under your user cache folder; daily rotation

---

## 📥 Install

Grab the build for your OS/CPU from **[Releases](https://github.com/TrueFix/isup/releases)** (`amd64` = Intel/AMD 64-bit, `arm64` = Apple Silicon & ARM64).

```sh
# Linux example
tar -xzf isup-*-linux-amd64.tar.gz
./isup
```

Windows: run `isup.exe`. macOS/Linux: `./isup`.

---

## 🚀 Usage

```sh
isup                                  # auto: dashboard in a TTY, logs elsewhere

isup -mode=terminal                   # 📊 dashboard + 🔔 notifications
isup -mode=terminal -notify=false     # dashboard only

isup -mode=headless                   # plain logs, no notifications
isup -mode=headless -notify=true      # logs + notifications
isup -mode=headless -verbose          # log every source each sample
```

**Quit:** `Ctrl+C` / SIGTERM · In dashboard: **`q`**, **Esc**, or **Ctrl+C**  
**Navigate sources:** ↑/↓ or **j/k**

| Flag | Default | |
| --- | --- | --- |
| `-interval` | `5s` | Time between samples |
| `-timeout` | `4s` | Max time for one full check round |
| `-successes` | `2` | Good samples needed for **UP** |
| `-failures` | `3` | Bad samples (all sources fail) for **DOWN** |
| `-log-dir` | OS cache/`isup` | Log + icon directory |
| `-log-retention` | `30` | Days of logs to keep |

More: `isup -help` · `isup -version`

---

## 🧠 How it decides

Every few seconds, all sources are checked **in parallel** (no overlapping rounds).

| State | Meaning |
| --- | --- |
| **UP** ✅ | At least one **web** check succeeds |
| **DEGRADED** ⚠️ | Public DNS works, but **every** web check fails → *Limited connectivity* |
| **DOWN** ❌ | **All** sources fail for `-failures` samples in a row |

One blocked provider or blocked `8.8.8.8` alone does **not** trigger “internet down.”

### Default sources

| | Target | Pass condition |
| --- | --- | --- |
| ☁️ Cloudflare DNS | `1.1.1.1` | Valid DNS answer for `example.com` |
| 🔵 Google DNS | `8.8.8.8` | Same |
| 🛡️ Quad9 DNS | `9.9.9.9` | Same |
| ☁️ Cloudflare | `cp.cloudflare.com/generate_204` | HTTP **204** |
| 🔵 Google | `gstatic.com/generate_204` | HTTP **204** |
| 🦊 Mozilla | Firefox captive-portal URL | HTTP 200 + expected body |
| 🍎 Apple | Apple test page | HTTP 200 + expected body |

Checks hit real endpoints (not ICMP ping). VPNs, proxies, and firewalls can change what you see — tune with custom flags if needed.

---

## 📊 Terminal dashboard

- Overall state, thresholds, and “checking…” live status
- Per-source latency and last error
- Recent sample strip + uptime-style stats
- Resizes with your terminal; scrolls on small screens

Auto mode uses the dashboard only when stdin/stdout are a real TTY (`TERM` ≠ `dumb`).

---

## 🔔 Notifications

On by default in **terminal** mode, off in **headless**. Toggle with `-notify=true|false`.

Needs a normal desktop session (D-Bus on Linux, etc.). Failures are logged; monitoring keeps running.

---

## 📝 Logs

Written to `isup-YYYY-MM-DD.log`:

| OS | Location |
| --- | --- |
| Linux | `~/.cache/isup` (or `$XDG_CACHE_HOME/isup`) |
| macOS | `~/Library/Caches/isup` |
| Windows | `%LocalAppData%\isup` |

State **changes** are logged (with duration of the previous state). Use `-verbose` for every sample.

---

## 🛠 Custom checks

Replace **all** defaults by repeating flags:

```sh
isup -endpoint=https://your.corp/health -endpoint=https://backup.corp/health
isup -dns=1.1.1.1 -dns=8.8.8.8
```

Custom HTTP endpoints should return **204**. `-dns` only configs test DNS reachability, not full web access.

---

## 🔨 Build from source

Requires **Go 1.26+**.

```sh
git clone https://github.com/TrueFix/isup.git
cd isup
go build -trimpath -o isup .
```

---

## 📄 License

[LICENSE](LICENSE)

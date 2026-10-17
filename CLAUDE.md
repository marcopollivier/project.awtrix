# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

Social counters (GitHub, YouTube, Instagram, LinkedIn) and live football on a Ulanzi TC001 (32×8 LED matrix) running **AWTRIX NG**. There are two runtimes that must stay in sync:

1. **On the clock (primary):** Berry scripts in `scripts/*.ax` run inside the firmware, fetch the data themselves and keep their state on the device. The clock needs nothing else running.
2. **The hub (optional):** the Go binary `awtrix-hub`. It installs and configures the scripts (`deploy`), runs a browser simulator of the clock, serves a metrics endpoint the scripts can read, and pushes ready-made apps (football is hub-only).

The user writes in Portuguese. User-facing text (CLI messages, logs, README, commit messages, PRs, issues) is Portuguese; code comments and identifiers are English.

## Commands

```sh
make build                 # bin/awtrix-hub
make test                  # go test ./...
make vet
make lint                  # gofmt + vet + golangci-lint at the CI's version (gosec flags marshaled secrets)
go test ./internal/scheduler -run TestCounterDeltaProgressAndMilestone   # one test
make test-scripts BERRY=/path/to/berry   # Berry scripts against NG stand-ins
make lint-scripts BERRY=/path/to/berry   # compile every .ax, @config only in the header
make dev                   # hub + simulator at http://localhost:7070
make mock                  # fake data for every source, simulator only, throwaway state
make replay                # recorded Vasco match in the simulator
make hooks                 # enable .githooks/pre-push (gitleaks, Go checks, Berry tests)

./bin/awtrix-hub deploy [--only counter,github] [--no-icons]   # install scripts on the clock
./bin/awtrix-hub set linkedin 7430                             # manual counter, also PATCHes the clock
```

Berry is not in Homebrew. Build it with `git clone https://github.com/berry-lang/berry && make -C berry`.

The pre-push hook scans the outgoing commits with gitleaks (blocks), runs gofmt/vet/test and golangci-lint (gosec included, same version as CI via `go run`) when Go changed (blocks), and runs the Berry tests when `scripts/` changed (only warns if Berry is missing; CI blocks). Do not bypass it with `--no-verify` unless the user asks.

Config is `config.yaml` (from `config.example.yaml`) and secrets are in `.env`. Both are gitignored. Hub state lives in `~/.awtrix-hub/`.

## Architecture

### Hub (Go)

`sources/*` → `scheduler` → `awtrix.Client` → targets (the simulator, or a clock).

- **Sources** (`internal/sources`) return either `Metrics`, which are raw counters, or ready `Apps`/`Notifications` (football).
- **`scheduler.counter`** turns a metric into an app: daily delta, milestones (`internal/milestones`), compact formatting (`internal/render`).
- **`state`** persists counters to `state.json`.
- **`Replay`** re-sends apps when a device's uptime drops, because a rebooted clock forgets pushed apps.

### AWTRIX NG API

`internal/awtrix` speaks the NG API:
- apps: `PUT /api/v1/apps/pushed/{name}`;
- removal: `DELETE /api/v1/apps/{name}`;
- notifications: `POST /api/v1/notifications`;
- also files, device, scripts and app config routes.

**NG validates strictly.** An unknown key answers 422, so `types.go` may only contain real NG payload keys (camelCase, milliseconds). AWTRIX 3 support was removed on purpose.

The simulator (`internal/sim`) imitates the same routes and the same strictness: it requires `application/json` and rejects unknown fields. Payload changes therefore show up there first. `web/matrix.js` reads the NG field names.

### Metrics endpoint

`internal/api` is served by `run` when `api.addr` is set:
- `GET /api/v1/metrics` returns everything;
- `GET /api/v1/metrics/{key}` returns `{"value":N,"age":S}`.

The age is computed hub-side, so clock skew does not matter. The scripts use the per-key route.

### Deploy ↔ scripts contract

`internal/deploy`:
- installs modules first;
- fills each script's `@config` from config/.env through `deploy.Settings`;
- reports compile errors from `GET /api/v1/apps`.

Every key in `deploy.Settings` must exist as a `# @config <key>` line in that script, or the clock rejects the PATCH. `TestScriptsMatchSettings` enforces this, so change both sides together.

Two rules about which keys are sent:
- empty secrets are not sent, so a token typed in the clock UI is not wiped;
- `source` is never sent, because it stays as the user chose it on the clock.
- TikTok's `refresh_token` is never sent. TikTok may rotate it on every refresh, so the clock and the hub each get their own from `awtrix-hub tiktok login [--hub]`; the clock's copy is written straight to its config, and a shared token would cut off whichever runtime did not refresh it.

### Scripts (Berry, `scripts/`)

`counter.ax` is a `@module` holding the `Counter` base class, the Berry twin of `scheduler.counter`. Each app subclasses it and only implements `direct(done)`, calling `done(value)` or `done(nil, httpStatus)`.

The base handles:
- the fallback: `source` = `auto` tries the hub, marks it down for 30 min on failure and fetches direct in the same cycle;
- backoff on failures (1, 2, 4… min, capped at the app interval; 400/401/403/429 wait the full interval);
- the day rollover;
- milestones and the party notification;
- graying out stale values.

Keys used by both runtimes must match: `github_followers`, `github_stars`, `youtube_subs`, `instagram_followers`, `linkedin_followers`, `tiktok_followers`.

`testdata/parity.json` holds vectors both sides test (keys, formatting, delta, bar, parties): `internal/*/parity_test.go` in Go, `scripts_test.be` in Berry. A change to shared behavior updates the vectors, so both test suites fail until both sides follow. The `parity` skill (`.claude/skills/parity`) maps each Go file to its Berry twin and lists the known divergences.

Firmware constraints that shape the code:
- **8 KB per HTTP response, 96 KB of memory for all scripts.** Use the `find`/`keep` options and `re.search`/`re.matchall`, not `json.load`. Build nothing in `draw()`: text pieces and the bar are prepared in `render()`. Regexes allow optional spaces (`": *"`) because GitHub and Google pretty-print.
- **Store scope:** apps pass their own `store` into the base (`self.st`) instead of the module calling `store`. That keeps it unambiguous whose settings and state are read. Module-level settings (hub URL, colors) are read at import into `counter.cfg`.
- **`@config` lines must sit in the header, before any code.** Removing a `@config` line deletes the user's value on the clock.
- **GitHub stars** use GraphQL (`privacy:PUBLIC`, `isFork:false`), which needs a token. The REST repo list is ~350 KB.

### Script test harness

`scripts/test/ng.be` stubs the NG runtime: `store`, `http` with find/keep, a tiny `re`, clock, display and notify. `run.be` loads the stubs before compiling the tests, because Berry resolves globals at compile time. `load()` strips `import counter` and uses the module set in `global.counter`.

The stubs are not the firmware. After changing a script, run `deploy` against the real clock and check for errors.

## Working with the real clock

Its address is `device.url` in `config.yaml`. The web UI has no password, by the user's choice.

- Logs: `GET /api/v1/logs?after=N` (34 lines, each script logs where a value came from).
- Script state: `GET /api/v1/apps/{name}/data`. The Instagram data includes the token, so don't echo it.
- Screen pixels: `GET /api/v1/display/screen`.

Reflashing: the chip has **16 MB** flash, so use `usb-awtrix-ng-16mb.bin`. The serial link is noisy, so use esptool at 115200 baud. A backup of the stock Ulanzi firmware is in `~/.awtrix-hub/backup/`.

`targets` in `config.yaml` should list only the simulator. Pushing apps to the clock would duplicate what the scripts show.

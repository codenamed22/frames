# Frame

A local-media streaming app and hands-on MPEG-DASH learning project.

**Milestone 3 in progress: persistent catalog and optional Cadence preparation.** Go prepares one selected video as a source-aware adaptive DASH/HLS ladder, preserves the original, and records it in SQLite. Shaka provides automatic or manual DASH quality selection and stream diagnostics. Cadence can now orchestrate preparation with a separate local worker. Folder scanning and the multi-video API/UI remain next slices; the current screen still serves the selected video.

## Requirements

- Git to clone this repository.
- Go 1.26 or newer.
- Node.js 24 LTS or newer and npm to build the frontend (Node.js 22.12+ also works with the current build tooling).
- FFmpeg and FFprobe on PATH, or explicit executable paths through flags. FFmpeg needs `libx264`, `aac`, the DASH muxer, and the scale filter's `reset_sar` option; FFmpeg 8+ is recommended.
- A trusted, unencrypted SDR video and adequate free disk space. HDR/wide-color BT.2020 input is rejected, not tone-mapped.

The default mode needs no Docker, cloud account, external database, or uploads. Optional Cadence mode uses Docker Desktop for the local workflow service. Fonts, icons, and the player are bundled locally. Internet access is needed for dependencies, container images, and the optional sample download, not watching.

## Set Up on macOS

Use a Mac supported by the current Go, Node.js, and FFmpeg releases. Install [Homebrew](https://brew.sh/) if needed, then run in Terminal:

```sh
brew install git go node@24 ffmpeg
export PATH="$(brew --prefix node@24)/bin:$PATH"
git clone https://github.com/codenamed22/frames.git
cd frames
go version
node --version
ffmpeg -version
ffprobe -version
npm --prefix web ci
npm --prefix web run build
go run ./cmd/streamer -library "$HOME/Movies" -input "$HOME/Movies/My Video.mp4"
```

Replace the movie path with a real SDR video. Add Homebrew's printed `node@24` PATH configuration to your shell profile to use it in future terminals. Apple Silicon and Intel Macs use different Homebrew prefixes; the command above handles both. If Git prompts for Apple's command-line tools, finish that installation and reopen Terminal.

## Set Up on Windows

Use a supported Windows 10/11 installation and PowerShell. Install the prerequisites with [WinGet](https://learn.microsoft.com/windows/package-manager/winget/), or use the installers from [Git](https://git-scm.com/downloads/win), [Go](https://go.dev/dl/), [Node.js](https://nodejs.org/), and an [FFmpeg Windows build](https://ffmpeg.org/download.html#build-windows):

```powershell
winget install --id Git.Git -e
winget install --id GoLang.Go -e
winget install --id OpenJS.NodeJS.LTS -e
winget install --id Gyan.FFmpeg -e
```

Close and reopen PowerShell so the new PATH entries take effect, then run:

```powershell
git clone https://github.com/codenamed22/frames.git
Set-Location frames
go version
node --version
ffmpeg -version
ffprobe -version
npm.cmd --prefix web ci
npm.cmd --prefix web run build
go run ./cmd/streamer -library "D:\Movies" -input "D:\Movies\My Video.mkv"
```

Replace the example drive and movie path. `npm.cmd` avoids PowerShell script-execution-policy issues without changing your security policy. If a downloaded FFmpeg archive is used, extract it and add its `bin` directory (containing both `ffmpeg.exe` and `ffprobe.exe`) to PATH, or pass the executable paths through `-ffmpeg` and `-ffprobe`. The default mode runs natively on Windows: WSL and Docker are not required.

On Windows ARM, check that compatible Go, Node.js, and FFmpeg builds are available; the example WinGet packages and runtime have not been certified on that architecture. macOS is locally tested; Windows setup is provided but native Windows playback/preparation still requires device verification. This is not a claim that every Mac or Windows device can encode these videos.

## First Run

Open <http://localhost:8080> in standalone Chrome after `Ready` appears. Preparation completes **before** the app is served and can take minutes or longer. Ctrl+C cancels preparation or stops the server. Restarting with the same unchanged source and cache reuses the finished output. Keep commands in the cloned repository directory; the default web, cache, and database paths are relative to it.

Only one selected video is served today. Folder browsing/scanning, subtitles, and HDR conversion are not implemented. No videos or generated assets are included in the clone; use your own file or the sample below.

If the port is occupied, add `-port 8081`. Other options:

```sh
go run ./cmd/streamer -input "/path/to/video.mkv" -cache "/path/to/streaming-cache" -port 8081
go run ./cmd/streamer -library "/path/to/library" -input "/path/to/library/Movies/video.mkv" -data "/path/to/frame.db"
go run ./cmd/streamer -input "/path/to/video.mp4" -ffmpeg "/path/to/ffmpeg" -ffprobe "/path/to/ffprobe"
go run ./cmd/streamer -help
```

The cache defaults to `.streamer/cache`. It holds streaming copies and thumbnails, not your original. There is no quota or automatic eviction yet: start with a short sample and monitor free space. Caches are checked for missing/empty segments before reuse. If a cache is damaged, select a new `-cache` directory to rebuild. Failed or cancelled preparation removes its own staging directory; a hard crash can leave an unserved `.preparing-*` directory. Completed caches are never deleted automatically.

The SQLite catalog defaults to `.streamer/frame.db`; override it with `-data`. `-library` defines the absolute root while catalog entries store only private root-relative paths. When omitted, the selected video's directory becomes the root. The selected video must be inside it. A catalog belongs to one library root; use a different `-data` path when switching roots, including when switching from your own movies to the demo. The catalog currently records prepared metadata and survives restarts, but it does not drive the single-video API yet. Renaming a file gives it a new opaque catalog ID.

## Optional Demo

A 52-second Sintel trailer is suitable for the first exercise:

```sh
curl --fail --location --create-dirs --output .streamer/samples/Sintel-trailer.mp4 https://media.w3.org/2010/05/sintel/trailer.mp4
go run ./cmd/streamer -library .streamer/samples -input .streamer/samples/Sintel-trailer.mp4 -data .streamer/demo.db
```

On Windows, run this in PowerShell after building the frontend:

```powershell
curl.exe --fail --location --create-dirs --output .streamer/samples/Sintel-trailer.mp4 https://media.w3.org/2010/05/sintel/trailer.mp4
go run ./cmd/streamer -library .streamer/samples -input .streamer/samples/Sintel-trailer.mp4 -data .streamer/demo.db
```

**Sample attribution:** Sintel trailer, (c) Blender Foundation, [durian.blender.org](https://durian.blender.org/), [Creative Commons Attribution 3.0](https://creativecommons.org/licenses/by/3.0/). Mirror: [W3C](https://media.w3.org/2010/05/sintel/trailer.mp4). Frame transcodes the sample and extracts a thumbnail. The sample, derived files, and screenshots are local ignored artifacts, not part of the source distribution.

## Optional Cadence Setup

First complete the native setup and download the demo above. Install and start [Docker Desktop](https://docs.docker.com/desktop/); on Windows enable its WSL 2 backend and Linux containers. Follow Docker's current OS, virtualization, licensing, and organization sign-in requirements. The Go worker and FFmpeg still run on the host, not inside Docker. Apple Silicon uses amd64 emulation for the pinned Cadence Web image.

The following single-line commands work in macOS Terminal and Windows PowerShell. Run them from the repository root. Start the local workflow services:

```sh
docker compose -f compose.cadence.yaml up -d
docker compose -f compose.cadence.yaml logs --tail=30 cadence
```

In a second terminal, start one worker:

```sh
go run ./cmd/worker -library .streamer/samples -cache .streamer/cadence-cache -data .streamer/cadence.db
```

In a third terminal, submit the sample and serve it:

```sh
go run ./cmd/streamer -cadence -library .streamer/samples -input .streamer/samples/Sintel-trailer.mp4 -cache .streamer/cadence-cache -data .streamer/cadence.db -port 8081
```

Wait for `Ready`, then open <http://localhost:8081>. Open <http://localhost:8088> and choose domain `frame-local` to inspect workflow history. If initial domain registration fails while containers are starting, check the logs and rerun the worker once Cadence is ready.

Both processes must use the same host and library/cache/database paths. Keep exactly one worker for that configuration. Ctrl+C in the streamer detaches without cancelling preparation; to cancel explicitly, leave the worker running and use:

```sh
go run ./cmd/worker -library .streamer/samples -cache .streamer/cadence-cache -data .streamer/cadence.db -cancel Sintel-trailer.mp4
```

Stop the Go processes with Ctrl+C and stop the services with `docker compose -f compose.cadence.yaml down`. Do not add `-v` unless you intend to delete workflow history. Docker services bind only to localhost and should not be exposed to the LAN. [Lab 04: Cadence](docs/cadence-lab-04.md) explains retries, cancellation, recovery limitations, and verified experiments.

## Troubleshooting

| Symptom                                     | Check                                                                                                          |
| ------------------------------------------- | -------------------------------------------------------------------------------------------------------------- |
| `go`, `node`, or `ffmpeg` not found         | Reopen the terminal after installation; verify versions and PATH.                                              |
| Unsupported scale option or missing encoder | Install FFmpeg 8+ with `libx264`, AAC, DASH, and `reset_sar` support.                                          |
| Web directory missing or blank app          | Run `npm --prefix web ci` and `npm --prefix web run build` from the repo root; use `npm.cmd` on Windows.       |
| Port already in use                         | Choose another `-port`, such as `8082`.                                                                        |
| Catalog belongs to another library          | Keep `-library` unchanged or select a new `-data` database.                                                    |
| Shaka error 3014 in VS Code                 | Open the URL in standalone Chrome; integrated-browser codec support is unreliable.                             |
| Cadence stays queued                        | Compare worker and streamer host/library/cache/data paths, and verify the worker is running.                   |
| Docker sign-in or virtualization error      | Complete Docker Desktop setup directly; do not paste credentials into logs or chat.                            |
| Phone cannot connect                        | Use the host's printed LAN IP, enable `-lan`, and check private-network firewall and Wi-Fi isolation settings. |

## Watch on Your LAN

Restart with explicit LAN access:

```sh
go run ./cmd/streamer -input "/path/to/video.mp4" -lan
```

Open a printed `LAN: http://...:8080` URL from a device on the same trusted network. `localhost` on your phone refers to your phone, not the host. Keep the host awake and allow the port through its firewall for private networks only. Guest Wi-Fi client isolation can prevent access. For a custom hostname add `-allow-host my-host.local`.

**No login exists yet. Anyone who can reach this port can watch the selected video.** Loopback-only access is the default. Host checks mitigate unexpected hostnames/DNS rebinding, but are not authentication. Do not use LAN mode on untrusted networks, forward the port on your router, or expose it publicly. Remote access, authorization, HTTPS, and rate limits need a separate implementation.

Auto prefers DASH/Shaka on desktop Chrome/Edge/Firefox and native HLS on Apple browsers when available. Both protocols use H.264 video and AAC audio. The Transport control lets you compare DASH and HLS. Explicit HLS also uses native playback in other capable browsers. Manual DASH can fail on older iOS without MediaSource support. Native HLS provides fewer statistics; bandwidth is unavailable rather than guessed.

Preparation selects eligible nominal 360p, 720p, and 1080p renditions at approximately 0.7, 2.5, and 5 Mbps. Selection accounts for sample aspect ratio and quarter-turn rotation. It skips upscales and emits one bounded source-size rendition for smaller inputs. Every rung uses integer multiples of a shared even-pixel base, keeping identical aspect ratios within pixel-rounding limits. All video renditions share aligned four-second boundaries and one AAC audio track. A 480p source may therefore have only a roughly 360p output; use a 720p or 1080p source to observe automatic switching.

The adaptive encoding profile has a new cache identity. Restarting with an existing source prepares its new ladder without overwriting or deleting the previous single-quality cache. This costs additional preparation time and disk space. The Quality menu controls DASH representations; selecting Auto re-enables Shaka ABR. Native HLS keeps browser-controlled automatic quality because its variant-selection API is not exposed consistently.

Use a standalone browser for playback. VS Code's integrated browser can advertise codec support but fail to initialize its audio decoder, producing Shaka error 3014 or native playback error 4. This was reproduced with both AAC and an experimental Opus stream; changing audio codecs did not resolve it. Open the same URL in standalone Chrome (verified locally) or try Safari. Error 3014 alone does not mean the media files are missing; the browser console includes the full playback error for diagnosis.

Resume uses the current browser's local storage, keyed by source version. It does not synchronize across devices and may be disabled by browser restrictions. Near-completed videos clear their resume position. Changing source path, size, modification time, or encoding profile gives a new cache identity.

## Development

Keep Go running, then start Vite in a second terminal:

```sh
npm --prefix web run dev
```

Vite binds to loopback and proxies `/api` and `/media` to port 8080. For a different backend port update [web/vite.config.ts](web/vite.config.ts). Use the built app through Go for LAN testing, not an exposed Vite development server.

After frontend edits, run `npm --prefix web run build` and reload the Go-served app. Build a native binary with `go build -o .streamer/bin/frame ./cmd/streamer` (use `.exe` on Windows). Run it from the project root or pass absolute `-web`, `-cache`, and `-input` paths. FFmpeg/FFprobe remain runtime dependencies; Node is unnecessary after building the UI.

For a Windows binary, run `go build -o .streamer/bin/frame.exe ./cmd/streamer`, then `.\.streamer\bin\frame.exe -library "D:\Movies" -input "D:\Movies\My Video.mkv"`. Build on the target host; generated caches and catalogs contain host-specific identities and should be rebuilt when moving a library to a different machine.

## Verification

```sh
go test ./...
go vet ./...
STREAMER_INTEGRATION=1 go test ./internal/media -v
STREAMER_INTEGRATION=1 go test ./internal/preparation -v
npm --prefix web run typecheck
npm --prefix web run lint
npm --prefix web run build
npm --prefix web test
```

In PowerShell set `$env:STREAMER_INTEGRATION="1"` before running the integration tests. They generate short 720p/1080p, silent, portrait, odd-sized, anamorphic, and rotated clips; decode every HLS variant; check shared audio and keyframes; inspect MPD alignment; verify source preservation; and reject deliberately damaged manifests and caches. FFmpeg builds without a DASH demuxer can still prepare DASH; browser tests verify its playback instead.

Playwright requires installed Google Chrome and a running server at localhost:8080 with a 720p or larger video longer than 25 seconds; use `.streamer/samples/adaptive-720p.mp4` when available. Set `STREAMER_URL` to test another address. Tests cover real playback, nonblank decoded frames, seeking, resume, manual quality selection, network-throttled ABR changes, transport switching, error recovery, and desktop/mobile layouts. Screenshots appear under `web/test-results`. `npm --prefix web run format` formats the frontend.

Phone-sized Chrome is **not** iOS certification. Before expanding the pipeline, verify actual Safari/iPhone/iPad: user-initiated playback with sound, seeking, completion, return/resume, and rotation. Native Windows launch and two physical LAN clients also need real-device checks; cross-compilation alone does not establish runtime compatibility.

## Code and Lessons

- [internal/media/media.go](internal/media/media.go): inspection, encoder arguments, staged preparation, cache validation.
- [internal/server/server.go](internal/server/server.go): API, confined asset delivery, MIME types, ranges, host checks.
- [internal/store/store.go](internal/store/store.go): SQLite schema migrations, library configuration, and persistent media records.
- [internal/preparation/workflow.go](internal/preparation/workflow.go): Cadence workflow orchestration and retry policies.
- [cmd/worker/main.go](cmd/worker/main.go): local worker, domain setup, and explicit cancellation.
- [cmd/streamer/main.go](cmd/streamer/main.go): native configuration and lifecycle.
- [web/src/Player.tsx](web/src/Player.tsx): playback, transport, metrics, resume.
- [web/src/App.tsx](web/src/App.tsx): library and watch navigation.
- [docs/streaming-lab.md](docs/streaming-lab.md): experiments and the next milestone.
- [docs/streaming-lab-02.md](docs/streaming-lab-02.md): adaptive representations, alignment, and ABR experiments.
- [docs/catalog-lab-03.md](docs/catalog-lab-03.md): catalog identity and restart persistence.
- [docs/cadence-lab-04.md](docs/cadence-lab-04.md): workflow history, retries, cancellation, and recovery experiments.

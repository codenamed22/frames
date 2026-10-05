# Frame

A local-media streaming app and hands-on MPEG-DASH learning project.

**Folder library and on-demand streaming.** Point Frame at a directory to discover videos recursively and browse their original folders. Selecting a video queues local preparation as an adaptive DASH/HLS stream; the library stays browsable while FFmpeg works. Originals are preserved and prepared metadata persists in SQLite. Shaka provides automatic or manual DASH quality selection and stream diagnostics. The original single-video mode, including optional Cadence preparation, remains available.

## Requirements

- Git to clone this repository.
- Go 1.26 or newer.
- Node.js 24 LTS or newer and npm to build the frontend (Node.js 22.12+ also works with the current build tooling).
- FFmpeg and FFprobe on PATH, or explicit executable paths through flags. FFmpeg needs `libx264`, `aac`, the DASH muxer, and the scale filter's `reset_sar` option; FFmpeg 8+ is recommended.
- A trusted, unencrypted SDR video and adequate free disk space. HDR/wide-color BT.2020 input is rejected, not tone-mapped.

The default mode needs no Docker, cloud account, external database, or uploads. Optional Cadence mode uses Docker Desktop for the local workflow service. Fonts, icons, and the player are bundled locally. Internet access is needed for dependencies, container images, and the optional sample download, not watching.

**Developing on a Mac and moving to a Windows PC?** Follow [Deploy From Your Mac to Windows](#deploy-from-your-mac-to-windows). Build the app on your Mac, transfer a ZIP, and run it against the Windows media folder. Windows does not need Git, Go, or Node.js for this deployment method.

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
go run ./cmd/streamer -library "$HOME/Movies"
```

Replace the library path with your video directory. Add Homebrew's printed `node@24` PATH configuration to your shell profile to use it in future terminals. Apple Silicon and Intel Macs use different Homebrew prefixes; the command above handles both. If Git prompts for Apple's command-line tools, finish that installation and reopen Terminal.

## Deploy From Your Mac to Windows

This is the recommended path when your working project is on a Mac. The Windows PC runs the server, stores the streaming cache, and performs video preparation; your Mac can be turned off afterward. This is a portable app, not an installer or an automatically started Windows service.

### 1. Check the Windows PC

- Use a Windows version supported by the current Go and FFmpeg releases; Windows 11 is recommended.
- Open **Settings > System > About > System type**. The commands below target **x64-based processors** (Intel or AMD), regardless of whether your Mac uses Apple Silicon or Intel.
- For **ARM-based processors**, change both `amd64` occurrences in the Mac build commands to `arm64` and install compatible Windows ARM FFmpeg binaries. That configuration has not been certified.
- Have a writable app directory and enough free disk space for streaming copies in addition to your original videos. Start with one short SDR video before preparing full movies. HDR conversion and subtitles are not supported yet.

### 2. Build the Windows Bundle on Your Mac

Run this in Terminal **from your current project directory**, after installing Go 1.26+ and Node.js as described in [Set Up on macOS](#set-up-on-macos). This builds your current files, including uncommitted changes. There is no need to push to GitHub first; a fresh clone on Windows would not include unpublished work.

```sh
# Use the local Homebrew Node installation, not a corporate npm wrapper.
export PATH="$(brew --prefix node@24)/bin:$PATH"
npm --prefix web ci
npm --prefix web run build

mkdir -p .streamer
bundle="$(mktemp -d "$PWD/.streamer/windows-bundle.XXXXXX")"
mkdir -p "$bundle/Frame/web"
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o "$bundle/Frame/frame.exe" ./cmd/streamer
cp -R web/dist "$bundle/Frame/web/dist"
ditto -c -k --keepParent "$bundle/Frame" "$bundle/Frame-windows-amd64.zip"
printf 'Transfer this ZIP to Windows: %s\n' "$bundle/Frame-windows-amd64.zip"
```

If Node is installed by another method, use that local installation instead of the Homebrew `export` line. Stop if any command fails; do not transfer a partially built bundle. Each run creates a fresh staging directory to avoid mixing old and new frontend assets.

Transfer the printed ZIP using a USB drive, file share, or another trusted transfer method. The ZIP contains only:

```text
Frame/
	frame.exe
	web/
		dist/
			index.html
			assets/...
```

Keep the executable and the entire `web/dist` directory together. **Do not copy your Mac's `.streamer` directory, SQLite database, cache, `node_modules`, or Mac executable to Windows.** Catalogs and cache identities are host/path-specific. Your original videos are not bundled: copy them separately if they are currently on your Mac, preserving the folder layout.

### 3. Install FFmpeg on Windows

Open **PowerShell on Windows** and run:

```powershell
winget install --id Gyan.FFmpeg -e
```

Close and reopen PowerShell, then check:

```powershell
ffmpeg -version
ffprobe -version
```

Use FFmpeg 8+ with `libx264`, AAC, and DASH support. If WinGet is unavailable, obtain a compatible build through [FFmpeg's Windows download links](https://ffmpeg.org/download.html#build-windows), extract it, and add its `bin` directory to PATH. Explicit executable paths are also supported; see below. Install standalone Google Chrome for the initial playback check. Neither Docker nor WSL is needed.

### 4. Extract and Launch on Windows

Assuming the transferred ZIP is in Downloads, run:

```powershell
Expand-Archive -LiteralPath "$HOME\Downloads\Frame-windows-amd64.zip" -DestinationPath "$HOME\Apps"
Set-Location "$HOME\Apps\Frame"
.\frame.exe -library "D:\Videos"
```

Replace `D:\Videos` with the **existing Windows folder containing your videos**. Keep quotes around paths with spaces. Do not pass `-input` when you want to browse the whole folder tree.

For example:

```text
D:\Videos\
	Hobbit Trilogy\
		01 - An Unexpected Journey.mkv
		02 - The Desolation of Smaug.mkv
		03 - The Battle of the Five Armies.mkv
```

After `Ready: http://localhost:8080` appears, open **http://localhost:8080 on the Windows PC**. You should see **Hobbit Trilogy**, then its three videos when you open the folder. Select one, wait for preparation, and press Play. The first preparation may take minutes or longer; browsing remains available. Originals are not modified.

Leave the PowerShell window open while using Frame. Stop it with **Ctrl+C**. To start again, repeat `Set-Location` and the `.\frame.exe` command; you do not need to rebuild, reinstall, or extract the ZIP again. Use **Rescan library** after adding or removing videos.

### 5. Choose Storage and Network Access

By default, the Windows catalog and generated streaming copies are created under `$HOME\Apps\Frame\.streamer`. To put them on another drive, use the same explicit paths on every launch:

```powershell
Set-Location "$HOME\Apps\Frame"
.\frame.exe -library "D:\Videos" -cache "D:\FrameData\cache" -data "D:\FrameData\frame.db"
```

Use a cache directory separate from the media root. Keep the chosen library root stable; if you change it, choose a new `-data` database. Keep only one Frame process running for a given database/cache. Caches have no automatic size limit or cleanup.

If FFmpeg is not on PATH, append `-ffmpeg "C:\Tools\ffmpeg\bin\ffmpeg.exe" -ffprobe "C:\Tools\ffmpeg\bin\ffprobe.exe"` to your launch command. If port 8080 is busy, append `-port 8081` and open http://localhost:8081 instead. If the UI is missing, confirm you are in the extracted `Frame` directory and that `web\dist\index.html` exists; the executable alone is insufficient.

To watch from your Mac or phone while **Windows hosts the library**, append `-lan` to the same launch command. Open the printed **LAN URL**, not `localhost`, on the other device. Allow the app through Windows Firewall for **private networks only** and keep the PC awake. There is no login: anyone who can reach the port can browse, prepare, and watch your videos. Never forward the port to the internet. See [Watch on Your LAN](#watch-on-your-lan).

### 6. Verify and Update

Before trusting a large collection, verify these on the Windows PC:

1. A folder opens and lists the expected videos.
2. A short video finishes preparation and plays with sound; seeking works.
3. Returning to the folder and reopening the video reuses the prepared copy.
4. After Ctrl+C and relaunching with the same paths, the library and prepared video still work.
5. Adding another video and clicking **Rescan library** makes it appear.

For updates, rebuild a fresh ZIP on the Mac, stop Frame on Windows, and extract the ZIP to a **separate temporary directory**. Replace only `frame.exe` and the complete `web\dist` directory in your installed `Frame` folder. Preserve the Windows `.streamer` directory (or your explicit `-cache` and `-data` locations), and restart with the same launch command. Do not replace the whole app folder if that would discard its data.

**Verification boundary:** Windows AMD64 cross-compilation has passed on macOS, and folder browsing/preparation/playback has been tested in desktop and mobile-sized Chrome on macOS. Native Windows execution still requires the checks above; cross-compilation is not proof of Windows runtime compatibility. Windows may flag a locally built, unsigned executable; verify its origin and follow your device's security policy rather than disabling protection globally.

## Build Directly on Windows (Alternative)

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
go run ./cmd/streamer -library "D:\Movies"
```

Replace the example drive and library path. `npm.cmd` avoids PowerShell script-execution-policy issues without changing your security policy. If a downloaded FFmpeg archive is used, extract it and add its `bin` directory (containing both `ffmpeg.exe` and `ffprobe.exe`) to PATH, or pass the executable paths through `-ffmpeg` and `-ffprobe`. The default mode runs natively on Windows: WSL and Docker are not required.

On Windows ARM, check that compatible Go, Node.js, and FFmpeg builds are available; the example WinGet packages and runtime have not been certified on that architecture. macOS is locally tested; Windows setup is provided but native Windows playback/preparation still requires device verification. This is not a claim that every Mac or Windows device can encode these videos.

## First Run

Open <http://localhost:8080> in standalone Chrome after `Ready` appears. In library mode, startup scans filenames without encoding the collection. For example, `D:\Movies\Hobbit Trilogy` appears as **Hobbit Trilogy**; opening it lists the videos inside. Nested folders and breadcrumbs preserve your directory structure. Search filters the current folder.

Select a video to prepare it. One encode runs at a time, with up to 32 waiting selections; duplicate selections share the same job. The player appears when preparation finishes, which can take minutes or longer for a full movie. You can return to the library while it runs. Prepared videos reuse their streaming copies and thumbnails. Failed or interrupted jobs can be retried. Ctrl+C cancels active preparation and stops the server; after restart, select interrupted videos again.

Use the **Rescan library** button after adding, changing, moving, or removing files. Scanning also runs at startup; there is no automatic filesystem watcher. Changed sources lose their ready state and missing sources disappear from browsing, without deleting their original files or old caches. Folders appear when they contain supported video filenames, including nested descendants. Empty folders are not shown.

Discovery recognizes `.mp4`, `.mkv`, `.mov`, `.webm`, `.avi`, `.m4v`, `.mpg`, `.mpeg`, `.ts`, and `.m2ts`, case-insensitively; actual codec support is checked during preparation. Symbolic links, `.streamer` directories, and the configured cache directory are skipped. The library root cannot itself be a symlink or the cache directory. Use trusted local media, and do not replace files or directory links while preparation is running.

Passing `-input` retains the original single-video mode: only that video is shown, and preparation completes **before** serving the app. Keep commands in the cloned repository directory; default web, cache, and database paths are relative to it. Subtitles, HDR conversion, automatic movie metadata/artwork, and an installer are not implemented. No videos or generated assets are included in the clone; use your own files or the sample below.

If the port is occupied, add `-port 8081`. Other options:

```sh
go run ./cmd/streamer -input "/path/to/video.mkv" -cache "/path/to/streaming-cache" -port 8081
go run ./cmd/streamer -library "/path/to/library" -input "/path/to/library/Movies/video.mkv" -data "/path/to/frame.db"
go run ./cmd/streamer -input "/path/to/video.mp4" -ffmpeg "/path/to/ffmpeg" -ffprobe "/path/to/ffprobe"
go run ./cmd/streamer -help
```

The cache defaults to `.streamer/cache`. It holds streaming copies and thumbnails, not your original. There is no quota or automatic eviction yet: start with a short sample and monitor free space. Caches are checked for missing/empty segments before reuse. If a cache is damaged, select a new `-cache` directory to rebuild. Failed or cancelled preparation removes its own staging directory; a hard crash can leave an unserved `.preparing-*` directory. Completed caches are never deleted automatically.

The SQLite catalog defaults to `.streamer/frame.db`; override it with `-data`. `-library` defines the absolute root while catalog entries store root-relative paths. When omitted in single-video mode, the selected video's directory becomes the root. The selected video must be inside it. A catalog belongs to one library root; use a different `-data` path when switching roots, including when switching from your own movies to the demo. The catalog drives folder listings, preparation states, and multi-video playback. Browser responses contain relative paths and opaque IDs, not absolute filesystem paths. Renaming a file gives it a new opaque catalog ID. Run only one library server per catalog/cache configuration.

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

Cadence remains a single-selected-video mode and requires `-input`. Folder-library preparation uses the built-in local queue; omit `-cadence` for browsing a collection.

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
go run ./cmd/streamer -library "/path/to/videos" -lan
```

Open a printed `LAN: http://...:8080` URL from a device on the same trusted network. `localhost` on your phone refers to your phone, not the host. Keep the host awake and allow the port through its firewall for private networks only. Guest Wi-Fi client isolation can prevent access. For a custom hostname add `-allow-host my-host.local`.

**No login exists yet. Anyone who can reach this port can browse the library, queue preparation, rescan, and watch its videos.** Loopback-only access is the default. Host and same-origin mutation checks are not authentication. Do not use LAN mode on untrusted networks, forward the port on your router, or expose it publicly. Remote access, authorization, HTTPS, and rate limits need a separate implementation.

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

For a Windows binary, run `go build -o .streamer/bin/frame.exe ./cmd/streamer`, then `.\.streamer\bin\frame.exe -library "D:\Movies"`. Build on the target host; generated caches and catalogs contain host-specific identities and should be rebuilt when moving a library to a different machine.

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

The separate folder suite uses a library-mode server with a `Hobbit Trilogy` folder containing three short test videos, including `01 - An Unexpected Journey.mp4` and a filename containing `02`. Run `STREAMER_LIBRARY=1 STREAMER_URL=http://localhost:8082 npm --prefix web test -- library.spec.ts`. It verifies folder navigation, search, on-demand preparation, decoded video pixels, browser history/reload, rescanning, retry states, and desktop/mobile screenshots. Use synthetic fixtures, not actual movie files. Backend tests cover discovery, reconciliation, duplicate queueing, API boundaries, and asset access. The updated streamer cross-builds for Windows AMD64; native Windows playback remains unverified.

Phone-sized Chrome is **not** iOS certification. Before expanding the pipeline, verify actual Safari/iPhone/iPad: user-initiated playback with sound, seeking, completion, return/resume, and rotation. Native Windows launch and two physical LAN clients also need real-device checks; cross-compilation alone does not establish runtime compatibility.

## Code and Lessons

- [internal/media/media.go](internal/media/media.go): inspection, encoder arguments, staged preparation, cache validation.
- [internal/server/server.go](internal/server/server.go): API, confined asset delivery, MIME types, ranges, host checks.
- [internal/store/store.go](internal/store/store.go): SQLite schema migrations, library configuration, and persistent media records.
- [internal/library/library.go](internal/library/library.go): recursive discovery and source reconciliation.
- [internal/library/manager.go](internal/library/manager.go): bounded local preparation queue and catalog publication.
- [internal/preparation/workflow.go](internal/preparation/workflow.go): Cadence workflow orchestration and retry policies.
- [cmd/worker/main.go](cmd/worker/main.go): local worker, domain setup, and explicit cancellation.
- [cmd/streamer/main.go](cmd/streamer/main.go): native configuration and lifecycle.
- [web/src/Player.tsx](web/src/Player.tsx): playback, transport, metrics, resume.
- [web/src/App.tsx](web/src/App.tsx): library and watch navigation.
- [web/src/Library.tsx](web/src/Library.tsx): folder browsing, preparation states, and per-video selection.
- [docs/streaming-lab.md](docs/streaming-lab.md): experiments and the next milestone.
- [docs/streaming-lab-02.md](docs/streaming-lab-02.md): adaptive representations, alignment, and ABR experiments.
- [docs/catalog-lab-03.md](docs/catalog-lab-03.md): catalog identity and restart persistence.
- [docs/cadence-lab-04.md](docs/cadence-lab-04.md): workflow history, retries, cancellation, and recovery experiments.

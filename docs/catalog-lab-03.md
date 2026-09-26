# Lab 03: A Persistent Catalog

The streaming cache answers “which bytes can this browser play?” A catalog answers a different question: “which source files does the application know about, and what is their current state?” Frame now stores the first catalog record in SQLite before expanding to folder scanning and background jobs.

## Identity Boundaries

Frame keeps three values separate:

- The **media ID** is an opaque hash of the root-relative source path. It remains stable when that file's contents change. Renaming the file creates a new item.
- The **source fingerprint** covers the absolute source path, size, and modification time. It changes when the source is replaced or edited.
- The **ready version** also includes the encoding profile. It changes when the source changes or Frame changes its preparation settings.

This separation lets a future rescan update one catalog item, invalidate stale playback, and prepare a new immutable output URL without exposing a host filesystem path to clients.

## Inspect Persistence

Run a selected video with an explicit root and database:

```sh
go run ./cmd/streamer \
  -library "/path/to/library" \
  -input "/path/to/library/Movies/video.mkv" \
  -data ".streamer/frame.db"
```

Stop and run the same command again. Preparation reuses the validated media cache, while SQLite reopens the same catalog record. The database uses a versioned schema, WAL journaling, foreign-key checks, a busy timeout, and one connection in this single-process application.

The current UI still reads the selected video from `/api/video`. [Lab 04](cadence-lab-04.md) adds an optional Cadence workflow and one host-local worker instead of building a SQLite preparation queue. Recursive scanning and catalog APIs remain future slices. Each process uses its own SQLite connection; WAL and the busy timeout support worker/application access. An existing catalog now rejects changing its library root.

## Stored State

Each record includes its opaque ID, private relative source path, source fingerprint, probed media metadata, source availability, preparation state, ready output version and URLs, generated size, error text, and update time. Ready records must have complete DASH, HLS, and poster paths.

Original files remain outside SQLite and are never modified. Generated media remains under the separate cache root. The database contains private host metadata and is not served as a web asset.
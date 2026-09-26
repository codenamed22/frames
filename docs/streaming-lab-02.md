# Lab 02: Adaptive Quality

## The Ladder

One presentation can contain multiple encodings of the same video. DASH calls each encoding a **Representation** and groups interchangeable video representations in one **AdaptationSet**. Frame chooses from these bounding boxes:

| Name | Maximum dimensions | Target/max bitrate |
| --- | --- | ---: |
| 360p | 640 x 360 | 0.7 Mbps |
| 720p | 1280 x 720 | 2.5 Mbps |
| 1080p | 1920 x 1080 | 5 Mbps |

A rung is skipped when fitting the source inside its box would require upscaling. Selection uses square-pixel display dimensions after quarter-turn rotation, not just stored pixel dimensions. Sources smaller than 360p receive one even-sized, source-bounded rendition.

The smallest rung is rounded down to even dimensions. Higher rungs use integer multiples of that same base so their square-pixel aspect ratios remain identical: FFmpeg's DASH muxer rejects inconsistent aspect ratios within a video adaptation set. For example, a 720 x 1280 portrait source produces 202 x 360, 404 x 720, and 606 x 1080. This preserves source aspect ratio to the base rung's pixel-rounding precision. Extremely narrow sources that cannot fit at least two pixels in each dimension are rejected.

Audio is encoded once as 128 kbps stereo AAC and shared by every video representation. This avoids storing and downloading duplicate audio solely because video quality changes.

## Inspect the Manifest

Prepare a 720p or 1080p SDR source, open the DASH manifest, and find the video `AdaptationSet`. Compare each `Representation`:

1. `width`, `height`, and `bandwidth` describe the available choices.
2. Each representation has its own initialization and media segment files.
3. The video `SegmentTimeline` values are identical across representations.
4. The audio adaptation set contains one representation, not one per video quality.

Aligned timelines are necessary but not sufficient by themselves. Each representation also uses closed GOPs, disables scene-cut keyframe drift, and forces keyframes every four seconds. That gives the player equivalent random-access points when switching.

## Observe Automatic Selection

Open standalone Chrome developer tools and select a constrained Network profile before playback. In the Network panel, filter for `chunk-stream`:

1. Start below 0.7 Mbps and observe buffering pressure or the lowest representation.
2. Raise throughput above 2.5 Mbps and leave it there long enough for Shaka's estimate and existing buffer to react.
3. Note changes in the stream number requested for video segments.
4. Compare decoded resolution, buffer ahead, and estimated bandwidth in Frame's statistics panel.

ABR does not switch immediately after a network-profile change. Already-buffered segments continue playing, throughput estimates need new request samples, and the player protects against oscillating between qualities. Native HLS controls its own selection and exposes less telemetry.

Frame starts ABR with a conservative 1 Mbps estimate and ignores the browser's coarse Network Information estimate. The statistics panel distinguishes estimated connection bandwidth from the current representation bitrate, reports the latest measured segment request, and records recent automatic or manual quality changes.

Use the Quality menu to choose a DASH representation. Manual mode disables ABR and clears buffered video beyond a small safety margin so the selected rendition appears promptly without restarting playback. Returning to Auto re-enables ABR. Native HLS remains browser-controlled, so the menu is disabled there.

The browser suite uses Chrome's network emulation to verify a real stream-0 to stream-1 upgrade on fast bandwidth and a stream-1 to stream-0 downshift after bandwidth is constrained and playback seeks beyond its buffer.

## Storage Cost

Prepared storage includes every video rendition plus one audio rendition. A bitrate-based planning estimate for one hour with all three rungs is:

$$
\frac{(0.7 + 2.5 + 5.0 + 0.128)\ \text{Mbit/s} \times 3600\ \text{s}}{8}
\approx 3.75\ \text{GB}
$$

Actual size varies with source complexity, encoder behavior, and container overhead; this is not an enforced storage bound. A viewer downloads one video representation at a time, but the host stores all eligible representations. The adaptive profile uses a new cache identity, leaving previous single-quality output untouched until you remove it explicitly.
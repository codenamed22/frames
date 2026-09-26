import { useEffect, useRef, useState } from "react";
import { Activity, ArrowUpRight, Play, RefreshCw } from "lucide-react";
import shaka from "shaka-player";
import {
  formatBytes,
  formatTime,
  readProgress,
  saveProgress,
  type Video,
} from "./media";

type Protocol = "auto" | "dash" | "hls";
interface QualityOption {
  id: number;
  width: number;
  height: number;
  bandwidth: number;
}
interface QualityChange extends QualityOption {
  at: number;
  automatic: boolean;
}
interface Metrics {
  buffer: number;
  current: number;
  width: number;
  height: number;
  dropped: number;
  decoded: number;
  bandwidth: number | null;
  streamBandwidth: number | null;
  bytesDownloaded: number | null;
  lastSegmentMs: number | null;
  lastSegmentBytes: number | null;
}
const emptyMetrics: Metrics = {
  buffer: 0,
  current: 0,
  width: 0,
  height: 0,
  dropped: 0,
  decoded: 0,
  bandwidth: null,
  streamBandwidth: null,
  bytesDownloaded: null,
  lastSegmentMs: null,
  lastSegmentBytes: null,
};

export function Player({ video }: { video: Video }) {
  const element = useRef<HTMLVideoElement>(null);
  const playerRef = useRef<shaka.Player | null>(null);
  const teardown = useRef<Promise<void>>(Promise.resolve());
  const lastPosition = useRef(readProgress(video.id));
  const qualityMode = useRef("auto");
  const [mode, setMode] = useState<Protocol>("auto");
  const [protocol, setProtocol] = useState("Connecting");
  const [ready, setReady] = useState(false);
  const [started, setStarted] = useState(false);
  const [error, setError] = useState("");
  const [attempt, setAttempt] = useState(0);
  const [metrics, setMetrics] = useState<Metrics>(emptyMetrics);
  const [statsOpen, setStatsOpen] = useState(true);
  const [speed, setSpeed] = useState("1");
  const [quality, setQuality] = useState("auto");
  const [qualities, setQualities] = useState<QualityOption[]>([]);
  const [activeQuality, setActiveQuality] = useState<QualityOption | null>(
    null,
  );
  const [qualityChanges, setQualityChanges] = useState<QualityChange[]>([]);

  useEffect(() => {
    const media = element.current!;
    let cancelled = false;
    let player: shaka.Player | null = null;
    let timer: ReturnType<typeof setInterval> | undefined;
    let restored = false;
    let activeVariantID: number | null = null;
    const requestEpoch = performance.now();
    const restore = () => {
      if (restored || !Number.isFinite(media.duration) || media.duration <= 0)
        return;
      restored = true;
      const progress = lastPosition.current;
      if (progress > 0 && progress < media.duration - 3)
        media.currentTime = progress;
    };
    const remember = () => {
      if (
        media.readyState < 1 ||
        !Number.isFinite(media.duration) ||
        media.duration <= 0
      )
        return;
      lastPosition.current =
        media.currentTime >= media.duration - 3 ? 0 : media.currentTime;
      saveProgress(video.id, media.currentTime, media.duration);
    };
    const reportError = (reason: unknown) => {
      if (cancelled) return;
      console.error("Playback error", reason);
      const detail =
        reason && typeof reason === "object" && "code" in reason
          ? ` (code ${String(reason.code)})`
          : "";
      setError(
        reason instanceof Error
          ? reason.message
          : `Playback failed${detail}. The browser could not load or decode this stream. Try opening this address in Chrome or Safari.`,
      );
      setReady(false);
    };
    const nativeError = () => reportError(media.error);

    const setup = teardown.current.then(async () => {
      if (cancelled) return;
      setReady(false);
      setStarted(false);
      setError("");
      setMetrics(emptyMetrics);
      setSpeed("1");
      setQuality("auto");
      qualityMode.current = "auto";
      setQualities([]);
      setActiveQuality(null);
      setQualityChanges([]);
      media.playbackRate = 1;
      media.addEventListener("loadedmetadata", restore);
      media.addEventListener("durationchange", restore);
      media.addEventListener("pause", remember);
      media.addEventListener("ended", remember);
      window.addEventListener("pagehide", remember);
      try {
        shaka.polyfill.installAll();
        const nativeHLS = Boolean(
          media.canPlayType("application/vnd.apple.mpegurl"),
        );
        const preferNativeHLS =
          mode === "hls" ||
          (mode === "auto" &&
            (navigator.vendor === "Apple Computer, Inc." ||
              !shaka.Player.isBrowserSupported()));
        if (preferNativeHLS && nativeHLS) {
          setProtocol("HLS / Native");
          media.addEventListener("error", nativeError);
          media.src = video.hls;
          media.load();
          setReady(true);
        } else {
          if (!shaka.Player.isBrowserSupported())
            throw new Error(
              "This browser cannot play DASH. Try HLS in Safari or a current desktop browser.",
            );
          player = new shaka.Player();
          playerRef.current = player;
          player.configure({
            abr: {
              useNetworkInformation: false,
              defaultBandwidthEstimate: 1_000_000,
              switchInterval: 4,
            },
          });
          const refreshVariants = () => {
            if (!player || cancelled) return;
            const tracks = player
              .getVariantTracks()
              .map(({ id, width, height, bandwidth }) => ({
                id,
                width: width ?? 0,
                height: height ?? 0,
                bandwidth,
              }))
              .filter((track) => track.width > 0 && track.height > 0)
              .sort((left, right) => left.height - right.height);
            setQualities(tracks);
            const active = player
              .getVariantTracks()
              .find((track) => track.active && (track.height ?? 0) > 0);
            if (!active) return;
            const current = {
              id: active.id,
              width: active.width ?? 0,
              height: active.height ?? 0,
              bandwidth: active.bandwidth,
            };
            setActiveQuality(current);
            if (active.id !== activeVariantID) {
              activeVariantID = active.id;
              setQualityChanges((changes) =>
                [
                  {
                    ...current,
                    at: media.currentTime,
                    automatic: qualityMode.current === "auto",
                  },
                  ...changes,
                ].slice(0, 4),
              );
            }
          };
          player.addEventListener("error", (event: Event) =>
            reportError((event as unknown as { detail: unknown }).detail),
          );
          player.addEventListener("adaptation", refreshVariants);
          player.addEventListener("variantchanged", refreshVariants);
          await player.attach(media);
          if (cancelled) return;
          setProtocol(mode === "hls" ? "HLS / Shaka" : "DASH / Shaka");
          await player.load(
            mode === "hls" ? video.hls : video.dash,
            lastPosition.current || undefined,
          );
          refreshVariants();
          restore();
        }
        if (cancelled) return;
        timer = setInterval(() => {
          let buffer = 0;
          for (let index = 0; index < media.buffered.length; index++) {
            if (
              media.buffered.start(index) <= media.currentTime &&
              media.buffered.end(index) >= media.currentTime
            )
              buffer = media.buffered.end(index) - media.currentTime;
          }
          const quality = media.getVideoPlaybackQuality?.();
          const stats = player?.getStats();
          const bandwidth = stats?.estimatedBandwidth;
          const segmentRequests = performance
            .getEntriesByType("resource")
            .filter(
              (entry) =>
                entry.startTime >= requestEpoch &&
                entry.name.includes("chunk-stream"),
            ) as PerformanceResourceTiming[];
          const lastSegment = segmentRequests.at(-1);
          setMetrics({
            buffer,
            current: media.currentTime,
            width: media.videoWidth,
            height: media.videoHeight,
            dropped: quality?.droppedVideoFrames ?? 0,
            decoded: quality?.totalVideoFrames ?? 0,
            bandwidth:
              bandwidth && Number.isFinite(bandwidth) ? bandwidth / 1e6 : null,
            streamBandwidth:
              stats?.streamBandwidth && Number.isFinite(stats.streamBandwidth)
                ? stats.streamBandwidth / 1e6
                : null,
            bytesDownloaded:
              stats && Number.isFinite(stats.bytesDownloaded)
                ? stats.bytesDownloaded
                : null,
            lastSegmentMs: lastSegment?.duration ?? null,
            lastSegmentBytes:
              lastSegment && lastSegment.encodedBodySize > 0
                ? lastSegment.encodedBodySize
                : null,
          });
          remember();
        }, 1000);
      } catch (reason) {
        reportError(reason);
      }
    });

    return () => {
      cancelled = true;
      clearInterval(timer);
      remember();
      media.removeEventListener("loadedmetadata", restore);
      media.removeEventListener("durationchange", restore);
      media.removeEventListener("pause", remember);
      media.removeEventListener("ended", remember);
      media.removeEventListener("error", nativeError);
      window.removeEventListener("pagehide", remember);
      teardown.current = setup
        .then(async () => {
          if (playerRef.current === player) playerRef.current = null;
          if (player) await player.destroy();
          media.pause();
          media.removeAttribute("src");
          media.load();
        })
        .catch(() => undefined);
    };
  }, [video, mode, attempt]);

  async function play() {
    try {
      await element.current?.play();
    } catch (reason) {
      setError(
        reason instanceof Error ? reason.message : "Playback could not start.",
      );
    }
  }

  function selectQuality(value: string) {
    setQuality(value);
    qualityMode.current = value;
    const player = playerRef.current;
    if (!player) return;
    if (value === "auto") {
      player.configure({ abr: { enabled: true } });
      return;
    }
    const track = player
      .getVariantTracks()
      .find((candidate) => candidate.id === Number(value));
    if (!track) return;
    player.configure({ abr: { enabled: false } });
    player.selectVariantTrack(track, true, 2);
  }

  return (
    <section className="watch-layout" aria-label="Video player">
      <div className="playback-column">
        <div className="video-stage">
          <video
            ref={element}
            controls
            playsInline
            preload="metadata"
            poster={video.poster}
            onCanPlay={() => setReady(true)}
            onPlay={() => setStarted(true)}
            onEnded={() => setStarted(false)}
            aria-label={video.title}
          />
          {!started && ready && !error && (
            <button
              className="start-play"
              onClick={play}
              aria-label="Play video"
            >
              <Play size={30} fill="currentColor" />
            </button>
          )}
          {!ready && !error && (
            <div className="player-loading" role="status">
              <span className="spinner" />
              Loading stream
            </div>
          )}
          {error && (
            <div className="player-error" role="alert">
              <p>{error}</p>
              <button
                className="primary-button"
                onClick={() => setAttempt(attempt + 1)}
              >
                <RefreshCw size={16} />
                Retry playback
              </button>
            </div>
          )}
        </div>
        <div className="playback-toolbar">
          <span className="playing-protocol">
            <i />
            {protocol}
          </span>
          <label className="quality-control">
            Quality
            <select
              aria-label="Video quality"
              value={quality}
              disabled={qualities.length === 0}
              onChange={(event) => selectQuality(event.target.value)}
            >
              <option value="auto">
                {qualities.length === 0 ? "Automatic" : "Auto"}
              </option>
              {qualities.map((option) => (
                <option key={option.id} value={option.id}>
                  {option.height}p
                </option>
              ))}
            </select>
          </label>
          <label className="speed-control">
            Speed
            <select
              aria-label="Playback speed"
              value={speed}
              onChange={(event) => {
                setSpeed(event.target.value);
                if (element.current)
                  element.current.playbackRate = Number(event.target.value);
              }}
            >
              <option value="0.5">0.5x</option>
              <option value="1">1x</option>
              <option value="1.25">1.25x</option>
              <option value="1.5">1.5x</option>
              <option value="2">2x</option>
            </select>
          </label>
          <button
            className={`icon-button stats-toggle ${statsOpen ? "selected" : ""}`}
            aria-label="Stream statistics"
            aria-expanded={statsOpen}
            aria-controls="stream-stats"
            title="Stream statistics"
            onClick={() => setStatsOpen(!statsOpen)}
          >
            <Activity size={18} />
          </button>
        </div>
        <div className="transport-row">
          <span>Transport</span>
          <fieldset className="segmented-control">
            <legend className="sr-only">Streaming protocol</legend>
            {(["auto", "dash", "hls"] as const).map((value) => (
              <label key={value}>
                <input
                  type="radio"
                  name="protocol"
                  value={value}
                  checked={mode === value}
                  onChange={() => setMode(value)}
                />
                <span>{value.toUpperCase()}</span>
              </label>
            ))}
          </fieldset>
          <a
            className="manifest-link"
            href={protocol.startsWith("HLS") ? video.hls : video.dash}
            target="_blank"
            rel="noreferrer"
          >
            Manifest
            <ArrowUpRight size={15} />
          </a>
        </div>
      </div>
      {statsOpen && (
        <aside id="stream-stats" className="stream-stats">
          <div className="details-heading">
            <Activity size={17} />
            <h2>Stream statistics</h2>
          </div>
          <p className="eyebrow live-label">
            <i />
            LIVE
          </p>
          <dl className="stats-list">
            <div>
              <dt>Buffer ahead</dt>
              <dd data-testid="buffer">
                {metrics.buffer.toFixed(1)}
                <small> sec</small>
              </dd>
            </div>
            <div>
              <dt>Playback position</dt>
              <dd>
                {formatTime(metrics.current)}
                <small> / {formatTime(video.duration)}</small>
              </dd>
            </div>
            <div>
              <dt>Decoded resolution</dt>
              <dd className="small-stat">
                {metrics.width
                  ? `${metrics.width} x ${metrics.height}`
                  : "Pending"}
              </dd>
            </div>
            <div>
              <dt>Active quality</dt>
              <dd className="small-stat" data-testid="active-quality">
                {activeQuality
                  ? `${activeQuality.height}p / ${(activeQuality.bandwidth / 1e6).toFixed(1)} Mbps`
                  : "Browser controlled"}
              </dd>
            </div>
            <div>
              <dt>Quality mode</dt>
              <dd className="small-stat" data-testid="quality-mode">
                {qualities.length === 0
                  ? "Unavailable"
                  : quality === "auto"
                    ? "Auto (ABR)"
                    : "Manual"}
              </dd>
            </div>
            <div>
              <dt>Est. bandwidth</dt>
              <dd className="small-stat">
                {metrics.bandwidth === null
                  ? "Unavailable"
                  : `${metrics.bandwidth.toFixed(2)} Mbps`}
              </dd>
            </div>
            <div>
              <dt>Current stream</dt>
              <dd className="small-stat">
                {metrics.streamBandwidth === null
                  ? "Unavailable"
                  : `${metrics.streamBandwidth.toFixed(2)} Mbps`}
              </dd>
            </div>
            <div>
              <dt>Downloaded</dt>
              <dd className="small-stat">
                {metrics.bytesDownloaded === null
                  ? "Unavailable"
                  : formatBytes(metrics.bytesDownloaded)}
              </dd>
            </div>
            <div>
              <dt>Latest segment</dt>
              <dd className="small-stat" data-testid="last-segment">
                {metrics.lastSegmentMs === null
                  ? "Pending"
                  : `${Math.round(metrics.lastSegmentMs)} ms`}
                {metrics.lastSegmentBytes !== null && (
                  <small> / {formatBytes(metrics.lastSegmentBytes)}</small>
                )}
              </dd>
            </div>
            <div className="frame-stats">
              <div>
                <dt>Decoded frames</dt>
                <dd className="small-stat" data-testid="decoded-frames">
                  {metrics.decoded}
                </dd>
              </div>
              <div>
                <dt>Dropped frames</dt>
                <dd className="small-stat">{metrics.dropped}</dd>
              </div>
            </div>
          </dl>
          {qualityChanges.length > 0 && (
            <div className="quality-history">
              <h3>Quality changes</h3>
              <ol data-testid="quality-history">
                {qualityChanges.map((change, index) => (
                  <li key={`${change.id}-${change.at}-${index}`}>
                    <span>{change.height}p</span>
                    <small>
                      {change.automatic ? "Auto" : "Manual"} at{" "}
                      {formatTime(change.at)}
                    </small>
                  </li>
                ))}
              </ol>
            </div>
          )}
          <div className="stream-spec">
            <span>H.264</span>
            <span>{video.audio ? "AAC" : "SILENT"}</span>
            <span>VOD</span>
          </div>
        </aside>
      )}
    </section>
  );
}

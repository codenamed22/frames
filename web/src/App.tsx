import { useEffect, useState } from "react";
import {
  ArrowLeft,
  ArrowUpRight,
  Check,
  Clapperboard,
  Film,
  HardDrive,
  Play,
  RefreshCw,
  Search,
  Wifi,
} from "lucide-react";
import { Player } from "./Player";
import { Library, type LibraryListing } from "./Library";
import { formatBytes, formatTime, readProgress, type Video } from "./media";

function App() {
  const [library, setLibrary] = useState<LibraryListing | null>(null);
  const [video, setVideo] = useState<Video | null>(null);
  const [error, setError] = useState("");
  const [attempt, setAttempt] = useState(0);
  const [watching, setWatching] = useState(
    window.location.pathname === "/watch",
  );
  const [query, setQuery] = useState("");

  useEffect(() => {
    const controller = new AbortController();
    fetch("/api/library", { signal: controller.signal })
      .then(async (response) => {
        if (response.ok) {
          setLibrary((await response.json()) as LibraryListing);
          return null;
        }
        if (response.status === 404)
          response = await fetch("/api/video", { signal: controller.signal });
        if (!response.ok)
          throw new Error(`Media server returned ${response.status}.`);
        return (await response.json()) as Video;
      })
      .then(setVideo)
      .catch((reason: unknown) => {
        if (!controller.signal.aborted)
          setError(
            reason instanceof Error
              ? reason.message
              : "Media server unavailable.",
          );
      });
    return () => controller.abort();
  }, [attempt]);

  useEffect(() => {
    const onPopState = () => setWatching(window.location.pathname === "/watch");
    window.addEventListener("popstate", onPopState);
    return () => window.removeEventListener("popstate", onPopState);
  }, []);

  function navigate(watch: boolean) {
    window.history.pushState(null, "", watch ? "/watch" : "/");
    setWatching(watch);
    window.scrollTo({ top: 0 });
  }

  const progress = video ? readProgress(video.id) : 0;
  const matches = video?.title.toLowerCase().includes(query.toLowerCase());

  if (library) return <Library initial={library} />;

  return (
    <div className="app-shell">
      <a className="skip-link" href="#main">
        Skip to content
      </a>
      <header className="masthead">
        <a
          className="brand"
          href="/"
          onClick={(event) => {
            event.preventDefault();
            navigate(false);
          }}
          aria-label="Frame home"
        >
          <Clapperboard size={26} strokeWidth={1.7} />
          <span>
            FRAME<span className="brand-period">.</span>
          </span>
        </a>
        <nav aria-label="Main navigation">
          <a
            href="/"
            className={!watching ? "nav-link active" : "nav-link"}
            aria-current={!watching ? "page" : undefined}
            onClick={(event) => {
              event.preventDefault();
              navigate(false);
            }}
          >
            Library
          </a>
          {watching && (
            <span className="nav-link active" aria-current="page">
              Now playing
            </span>
          )}
        </nav>
        <div className="host-status">
          <Wifi size={15} />
          <span>Local server</span>
          <i />
        </div>
      </header>
      <main id="main">
        {error ? (
          <section className="empty-state" role="alert">
            <HardDrive size={36} />
            <h1>Server unavailable</h1>
            <p>{error}</p>
            <button
              className="primary-button"
              onClick={() => {
                setError("");
                setAttempt(attempt + 1);
              }}
            >
              <RefreshCw size={16} />
              Retry
            </button>
          </section>
        ) : !video ? (
          <section
            className="loading-state"
            aria-label="Loading video"
            role="status"
          >
            <div className="skeleton title-skeleton" />
            <div className="skeleton image-skeleton" />
            <span>Loading library...</span>
          </section>
        ) : watching ? (
          <>
            <div className="watch-heading">
              <button
                className="icon-button"
                onClick={() => navigate(false)}
                aria-label="Back to library"
                title="Back to library"
              >
                <ArrowLeft size={21} />
              </button>
              <div>
                <p className="eyebrow">NOW PLAYING</p>
                <h1>{video.title}</h1>
              </div>
            </div>
            <Player key={video.id} video={video} />
          </>
        ) : (
          <>
            <section className="collection-heading">
              <div>
                <p className="eyebrow">
                  PERSONAL LIBRARY <span className="count">01</span>
                </p>
                <h1>
                  Your collection<span className="accent">.</span>
                </h1>
              </div>
              <label className="search-field">
                <Search size={18} />
                <input
                  aria-label="Search library"
                  placeholder="Search your library"
                  value={query}
                  onChange={(event) => setQuery(event.target.value)}
                  type="search"
                />
              </label>
            </section>
            {matches ? (
              <section
                className="collection-layout"
                aria-label="Prepared video"
              >
                <article className="featured-film">
                  <button
                    className="poster-button"
                    onClick={() => navigate(true)}
                    aria-label={`Watch ${video.title}`}
                  >
                    <img
                      className="poster"
                      src={video.poster}
                      alt={`Frame from ${video.title}`}
                    />
                    <span className="poster-play">
                      <Play size={27} fill="currentColor" />
                    </span>
                    <span className="duration-tag">
                      {formatTime(video.duration)}
                    </span>
                    {progress > 0 && (
                      <span className="resume-track">
                        <span
                          style={{
                            width: `${Math.min(100, (progress / video.duration) * 100)}%`,
                          }}
                        />
                      </span>
                    )}
                  </button>
                  <div className="film-caption">
                    <div>
                      <span className="ready-label">
                        <Check size={13} />
                        READY TO WATCH
                      </span>
                      <h2>{video.title}</h2>
                    </div>
                    <button
                      className="primary-button"
                      onClick={() => navigate(true)}
                    >
                      <Play size={16} fill="currentColor" />
                      {progress > 0 ? "Resume" : "Watch now"}
                    </button>
                  </div>
                </article>
                <aside className="media-details">
                  <div className="details-heading">
                    <Film size={17} />
                    <h2>Media details</h2>
                  </div>
                  <dl className="metadata-list">
                    <div>
                      <dt>Duration</dt>
                      <dd>{formatTime(video.duration)}</dd>
                    </div>
                    <div>
                      <dt>Source resolution</dt>
                      <dd>
                        {video.width} x {video.height}
                      </dd>
                    </div>
                    <div>
                      <dt>Source codec</dt>
                      <dd>{video.codec.toUpperCase()}</dd>
                    </div>
                    <div>
                      <dt>Prepared video</dt>
                      <dd>H.264 / SDR</dd>
                    </div>
                    <div>
                      <dt>Prepared audio</dt>
                      <dd>{video.audio ? "AAC / Stereo" : "No audio"}</dd>
                    </div>
                    <div>
                      <dt>Source size</dt>
                      <dd>{formatBytes(video.bytes)}</dd>
                    </div>
                    <div>
                      <dt>Streaming copies</dt>
                      <dd>{formatBytes(video.preparedBytes)}</dd>
                    </div>
                  </dl>
                  <div className="manifest-links">
                    <p className="eyebrow">MANIFESTS</p>
                    <a href={video.dash} target="_blank" rel="noreferrer">
                      DASH{" "}
                      <span>
                        MPD <ArrowUpRight size={15} />
                      </span>
                    </a>
                    <a href={video.hls} target="_blank" rel="noreferrer">
                      HLS{" "}
                      <span>
                        M3U8 <ArrowUpRight size={15} />
                      </span>
                    </a>
                  </div>
                </aside>
              </section>
            ) : (
              <section className="empty-state">
                <Search size={28} />
                <h2>No matching videos</h2>
                <button className="text-button" onClick={() => setQuery("")}>
                  Clear search
                </button>
              </section>
            )}
          </>
        )}
      </main>
      <footer className="footer">
        <span>
          <HardDrive size={14} />
          PERSONAL COLLECTION
        </span>
        <span>
          STREAMING LAB <b>02</b>
        </span>
      </footer>
    </div>
  );
}

export default App;

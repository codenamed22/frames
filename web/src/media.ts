export interface Video {
  id: string;
  title: string;
  duration: number;
  width: number;
  height: number;
  codec: string;
  audio: boolean;
  bytes: number;
  preparedBytes: number;
  dash: string;
  hls: string;
  poster: string;
}

export function formatTime(seconds: number) {
  const total = Number.isFinite(seconds) ? Math.max(0, Math.floor(seconds)) : 0;
  const hours = Math.floor(total / 3600);
  const minutes = Math.floor(total / 60) % 60;
  return `${hours ? `${hours}:` : ""}${hours ? String(minutes).padStart(2, "0") : minutes}:${String(total % 60).padStart(2, "0")}`;
}

export function formatBytes(bytes: number) {
  if (!Number.isFinite(bytes) || bytes <= 0) return "0 MB";
  return bytes >= 1e9
    ? `${(bytes / 1e9).toFixed(2)} GB`
    : `${(bytes / 1e6).toFixed(1)} MB`;
}

export function readProgress(id: string) {
  try {
    const progress = Number(localStorage.getItem(`frame:progress:${id}`));
    return Number.isFinite(progress) && progress > 0 ? progress : 0;
  } catch {
    return 0;
  }
}

export function saveProgress(id: string, current: number, duration: number) {
  try {
    if (current < 2 || current >= duration - 3)
      localStorage.removeItem(`frame:progress:${id}`);
    else localStorage.setItem(`frame:progress:${id}`, String(current));
  } catch {
    return;
  }
}

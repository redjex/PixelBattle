export type PlayerStatistics = {
  placedPixels: number;
  repaintedPixels: number;
  currentPixels: number;
  dailyPlacedPixels: number;
  dailyRepaintedPixels: number;
  dailyColorsUsed: number;
  dailyUniqueCells: number;
};

let cachedStatistics: PlayerStatistics | null = null;
let preloadRequest: Promise<PlayerStatistics> | null = null;

function statisticsUrl() {
  const apiUrl = import.meta.env.VITE_API_URL ?? window.location.origin;
  return `${apiUrl}/api/boards/main/stats`;
}

function requestStatistics(initData: string, signal?: AbortSignal) {
  return fetch(statisticsUrl(), {
    cache: 'no-store',
    headers: { 'X-Telegram-Init-Data': initData },
    signal,
  }).then((response) => {
    if (!response.ok) throw new Error(`Statistics request failed: ${response.status}`);
    return response.json() as Promise<PlayerStatistics>;
  }).then((statistics) => {
    cachedStatistics = statistics;
    return statistics;
  });
}

export function getCachedStatistics() {
  return cachedStatistics;
}

export function preloadStatistics(initData: string) {
  if (cachedStatistics) return Promise.resolve(cachedStatistics);
  if (!preloadRequest) {
    preloadRequest = requestStatistics(initData).finally(() => {
      preloadRequest = null;
    });
  }
  return preloadRequest;
}

export function refreshStatistics(initData: string, signal?: AbortSignal) {
  return requestStatistics(initData, signal);
}

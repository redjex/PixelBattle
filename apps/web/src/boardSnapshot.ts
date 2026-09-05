import type { Pixel } from './types/pixel';

export type BoardSnapshot = { width: number; height: number; pixels: Pixel[] };
type CompactSnapshot = { width: number; height: number; pixels: Array<{ x: number; y: number; c: string; a?: string; f?: string }> };

let cachedPromise: Promise<BoardSnapshot> | null = null;
let cachedAt = 0;

const wait = (milliseconds: number) => new Promise((resolve) => window.setTimeout(resolve, milliseconds));

async function requestSnapshot(initData: string): Promise<BoardSnapshot> {
  const apiUrl = import.meta.env.VITE_API_URL ?? window.location.origin;
  const delays = [0, 220, 550, 1100];
  let lastError: unknown;
  for (const delay of delays) {
    if (delay) await wait(delay);
    try {
      const response = await fetch(`${apiUrl}/api/boards/main?compact=1`, {
        cache: 'no-store',
        headers: { 'X-Telegram-Init-Data': initData },
      });
      if (!response.ok) throw new Error(`Board request failed: ${response.status}`);
      const snapshot = await response.json() as CompactSnapshot;
      return {
        width: snapshot.width,
        height: snapshot.height,
        pixels: snapshot.pixels.map((pixel) => ({
          x: pixel.x,
          y: pixel.y,
          color: pixel.c,
          author: pixel.a ? { id: pixel.a } : undefined,
          frozenUntil: pixel.f,
        })),
      };
    } catch (error) {
      lastError = error;
    }
  }
  throw lastError ?? new Error('Board request failed');
}

export function loadBoardSnapshot(initData: string, force = false): Promise<BoardSnapshot> {
  if (!force && cachedPromise && (cachedAt === 0 || Date.now() - cachedAt < 5000)) return cachedPromise;
  const request = requestSnapshot(initData).then((snapshot) => {
    cachedAt = Date.now();
    return snapshot;
  });
  cachedPromise = request;
  void request.catch(() => {
    if (cachedPromise === request) {
      cachedPromise = null;
      cachedAt = 0;
    }
  });
  return request;
}

export function preloadBoardSnapshot(initData: string) {
  return loadBoardSnapshot(initData);
}

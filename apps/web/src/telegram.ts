export type TelegramDeviceOrientation = {
  isStarted?: boolean;
  absolute?: boolean;
  alpha?: number;
  beta?: number;
  gamma?: number;
  start: (
    params: { refresh_rate?: number; need_absolute?: boolean },
    callback?: (started: boolean) => void,
  ) => TelegramDeviceOrientation;
  stop: (callback?: (stopped: boolean) => void) => TelegramDeviceOrientation;
};

export type TelegramWebApp = {
  initData: string;
  initDataUnsafe?: { user?: { id: number; username?: string; first_name?: string; photo_url?: string } };
  platform?: string;
  ready: () => void;
  expand: () => void;
  disableVerticalSwipes?: () => void;
  enableVerticalSwipes?: () => void;
  isVerticalSwipesEnabled?: boolean;
  requestFullscreen?: () => void;
  exitFullscreen?: () => void;
  isFullscreen?: boolean;
  safeAreaInset?: { top: number; bottom: number; left: number; right: number };
  contentSafeAreaInset?: { top: number; bottom: number; left: number; right: number };
  onEvent?: (eventType: string, callback: () => void) => void;
  offEvent?: (eventType: string, callback: () => void) => void;
  DeviceOrientation?: TelegramDeviceOrientation;
  HapticFeedback?: {
    notificationOccurred: (type: 'error' | 'success' | 'warning') => void;
  };
  BackButton?: {
    show: () => void;
    hide: () => void;
    onClick: (callback: () => void) => void;
    offClick: (callback: () => void) => void;
  };
};

export function getTelegramWebApp(): TelegramWebApp | null { return window.Telegram?.WebApp ?? null; }

// Admin capabilities must be granted by the server after validating initData.

export type AppAccess = {
  testMode: boolean;
  isAdmin: boolean;
  accessAllowed: boolean;
  online: number | null;
  prizes: unknown[];
};

export class TelegramAuthenticationError extends Error {
  constructor() {
    super('Telegram authentication failed');
    this.name = 'TelegramAuthenticationError';
  }
}

function ensureAvailable(response: Response) {
  if (response.status === 401 || response.status === 403) throw new TelegramAuthenticationError();
  if (!response.ok) throw new Error(`Telegram service unavailable: ${response.status}`);
}

export async function fetchAppAccess(initData: string): Promise<AppAccess> {
  const apiUrl = import.meta.env.VITE_API_URL ?? window.location.origin;
  const response = await fetch(`${apiUrl}/api/boards/session`, {
    cache: 'no-store',
    headers: { 'X-Telegram-Init-Data': initData },
  });
  ensureAvailable(response);
  const payload = await response.json() as Partial<AppAccess>;
  const testMode = payload.testMode === true;
  const isAdmin = payload.isAdmin === true;
  const online = typeof payload.online === 'number' && Number.isFinite(payload.online) ? Math.max(0, payload.online) : null;
  const prizes = Array.isArray(payload.prizes) ? payload.prizes : [];
  return { testMode, isAdmin, accessAllowed: payload.accessAllowed !== false && (!testMode || isAdmin), online, prizes };
}

export async function authenticateTelegram(initData: string): Promise<AppAccess | null> {
  const apiUrl = import.meta.env.VITE_API_URL ?? window.location.origin;
  const response = await fetch(`${apiUrl}/api/auth/telegram`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ init_data: initData }) });
  if (response.status === 401 || response.status === 403) return null;
  ensureAvailable(response);
  return fetchAppAccess(initData);
}

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
  BackButton?: {
    show: () => void;
    hide: () => void;
    onClick: (callback: () => void) => void;
    offClick: (callback: () => void) => void;
  };
};

const adminIds = new Set([743086174, 6997207264]);

export function getTelegramWebApp(): TelegramWebApp | null { return window.Telegram?.WebApp ?? null; }

export function isTelegramAdmin(): boolean { return adminIds.has(window.Telegram?.WebApp?.initDataUnsafe?.user?.id ?? 0); }

export async function authenticateTelegram(initData: string): Promise<boolean> {
  const apiUrl = import.meta.env.VITE_API_URL ?? window.location.origin;
  const response = await fetch(`${apiUrl}/api/auth/telegram`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ init_data: initData }) });
  return response.ok;
}

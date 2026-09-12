export type RewardSoundMode = 'mine' | 'all' | 'off';

const STORAGE_KEY = 'pixelbattle:reward-sound-mode';
const DEFAULT_MODE: RewardSoundMode = 'mine';
let currentMode: RewardSoundMode | undefined;

export function getRewardSoundMode(): RewardSoundMode {
  if (currentMode) return currentMode;
  try {
    const stored = window.localStorage.getItem(STORAGE_KEY);
    currentMode = stored === 'mine' || stored === 'all' || stored === 'off' ? stored : DEFAULT_MODE;
  } catch {
    currentMode = DEFAULT_MODE;
  }
  return currentMode;
}

export function setRewardSoundMode(mode: RewardSoundMode) {
  currentMode = mode;
  try {
    window.localStorage.setItem(STORAGE_KEY, mode);
  } catch {
    // The choice still applies until the Mini App is closed when storage is unavailable.
  }
}

export function shouldPlayRewardSound(isOwnReward: boolean) {
  const mode = getRewardSoundMode();
  return mode === 'all' || (mode === 'mine' && isOwnReward);
}

function normalizeNickname(value: string | undefined) {
  return value?.trim().replace(/^@/, '').toLocaleLowerCase() ?? '';
}

export function isCurrentUserNickname(nickname: string) {
  const user = window.Telegram?.WebApp?.initDataUnsafe?.user;
  if (!user) return false;
  const normalizedNickname = normalizeNickname(nickname);
  if (!normalizedNickname) return false;
  const username = normalizeNickname(user.username);
  if (username && normalizedNickname === username) return true;
  const displayName = normalizeNickname([user.first_name, user.last_name].filter(Boolean).join(' '));
  return Boolean(displayName && normalizedNickname === displayName);
}

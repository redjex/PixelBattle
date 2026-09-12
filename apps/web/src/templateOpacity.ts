export type TemplateOpacity = 20 | 50 | 100;

const STORAGE_KEY = 'pixelbattle:template-opacity';
const DEFAULT_OPACITY: TemplateOpacity = 50;
let currentOpacity: TemplateOpacity | undefined;

export function getTemplateOpacity(): TemplateOpacity {
  if (currentOpacity) return currentOpacity;
  try {
    const stored = Number(window.localStorage.getItem(STORAGE_KEY));
    currentOpacity = stored === 20 || stored === 50 || stored === 100 ? stored : DEFAULT_OPACITY;
  } catch {
    currentOpacity = DEFAULT_OPACITY;
  }
  return currentOpacity;
}

export function setTemplateOpacity(opacity: TemplateOpacity) {
  currentOpacity = opacity;
  try {
    window.localStorage.setItem(STORAGE_KEY, String(opacity));
  } catch {
    // The in-memory choice remains active when persistent storage is unavailable.
  }
}

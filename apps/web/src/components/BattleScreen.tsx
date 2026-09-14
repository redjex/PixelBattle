import { useCallback, useEffect, useLayoutEffect, useRef, useState, type CSSProperties, type MouseEvent as ReactMouseEvent } from 'react';
import { GlassControls } from './GlassControls';
import { PixelBoard } from './PixelBoard';
import type { Pixel } from '../types/pixel';
import { SeasonStatus } from './SeasonStatus';
import { loadTemplate, removeTemplate, saveTemplateImage, saveTemplatePlacement, type TemplatePlacement } from '../templateStorage';
import { getTemplateOpacity } from '../templateOpacity';

const COLORS = [
  '#FF8080', '#FFCA73', '#FBFFA5', '#7CFF80', '#7EFFF2', '#84D0FF', '#8290FF', '#CD81FF', '#FF80D0', '#FDFDFD',
  '#FF0000', '#FF9D00', '#F2FF00', '#00FF07', '#00FFE6', '#009DFF', '#001EFF', '#9900FF', '#FF00A1', '#8A8A8A',
  '#870000', '#8D4E00', '#B6A700', '#009904', '#009687', '#00568C', '#001194', '#53008A', '#8E005A', '#000000',
] as const;
const SELECTED_COLOR_STORAGE_KEY = 'pixelbattle:selected-color';
const DEFAULT_COLOR = '#009DFF';
const localPalettePreview = import.meta.env.DEV
  && new URLSearchParams(window.location.search).get('preview') === 'palette';

function getSavedColor() {
  try {
    const saved = window.localStorage.getItem(SELECTED_COLOR_STORAGE_KEY)?.toUpperCase();
    if (saved && /^#[0-9A-F]{6}(?:[0-9A-F]{2})?$/.test(saved)) return saved.slice(0, 7);
  } catch {
    // Storage can be unavailable in restricted WebViews.
  }
  return DEFAULT_COLOR;
}

function hueToHex(hue: number) {
  const sector = Math.floor(hue / 60) % 6;
  const blend = Math.round(255 * (1 - Math.abs((hue / 60) % 2 - 1)));
  const channels = [
    [255, blend, 0], [blend, 255, 0], [0, 255, blend],
    [0, blend, 255], [blend, 0, 255], [255, 0, blend],
  ][sector];
  return `#${channels
    .map((value) => value.toString(16).padStart(2, '0'))
    .join('')}`.toUpperCase();
}

function hexToHue(hex: string) {
  const red = Number.parseInt(hex.slice(1, 3), 16) / 255;
  const green = Number.parseInt(hex.slice(3, 5), 16) / 255;
  const blue = Number.parseInt(hex.slice(5, 7), 16) / 255;
  const maximum = Math.max(red, green, blue);
  const difference = maximum - Math.min(red, green, blue);
  if (difference === 0) return 0;
  const hue = maximum === red
    ? ((green - blue) / difference) % 6
    : maximum === green
      ? (blue - red) / difference + 2
      : (red - green) / difference + 4;
  return Math.round((hue * 60 + 360) % 360);
}

function hexToPigment(hex: string) {
  const channels = [1, 3, 5].map((index) => Number.parseInt(hex.slice(index, index + 2), 16));
  const maximum = Math.max(...channels);
  const minimum = Math.min(...channels);
  if (maximum === 0) return 100;
  if (minimum === 255) return 0;
  return maximum === 255
    ? Math.round(50 * (1 - minimum / 255))
    : Math.round(50 + 50 * (1 - maximum / 255));
}

function pigmentToHex(hue: number, pigment: number) {
  const base = hueToHex(hue);
  const channels = [1, 3, 5].map((index) => Number.parseInt(base.slice(index, index + 2), 16));
  const strength = pigment <= 50 ? pigment / 50 : (100 - pigment) / 50;
  const mixed = channels.map((channel) => Math.round(pigment <= 50
    ? 255 + (channel - 255) * strength
    : channel * strength));
  return `#${mixed.map((channel) => channel.toString(16).padStart(2, '0')).join('')}`.toUpperCase();
}

async function writeClipboardText(value: string) {
  try {
    await navigator.clipboard.writeText(value);
    return true;
  } catch {
    const input = document.createElement('textarea');
    input.value = value;
    input.setAttribute('readonly', '');
    input.style.position = 'fixed';
    input.style.left = '-9999px';
    document.body.appendChild(input);
    input.select();
    try {
      return document.execCommand('copy');
    } finally {
      input.remove();
    }
  }
}

type PixelAuthor = NonNullable<Pixel['author']>;
type Inventory = { bombs: number; ice: number; freezeRemaining: number };
const PROFILE_CACHE_MS = 5000;
const profileCache = new Map<string, { profile: PixelAuthor; expiresAt: number }>();
const profileRequests = new Map<string, Promise<PixelAuthor | null>>();
const EMPTY_INVENTORY: Inventory = { bombs: 0, ice: 0, freezeRemaining: 0 };

function openTelegramProfile(event: ReactMouseEvent<HTMLAnchorElement>, username: string) {
  const telegram = window.Telegram?.WebApp;
  if (!telegram?.openTelegramLink) return;
  event.preventDefault();
  telegram.openTelegramLink(`https://t.me/${username}`);
}

function RollingCoordinate({ value }: { value: number }) {
  const [transition, setTransition] = useState({ from: value, to: value, revision: 0 });

  useLayoutEffect(() => {
    setTransition((current) => current.to === value
      ? current
      : { from: current.to, to: value, revision: current.revision + 1 });
  }, [value]);

  const increasing = transition.to > transition.from;
  const decreasing = transition.to < transition.from;
  const steps = Math.abs(transition.to - transition.from);
  const first = Math.min(transition.from, transition.to);
  const sequence = Array.from({ length: steps + 1 }, (_, index) => first + index);
  const width = Math.max(String(transition.from).length, String(transition.to).length);

  return (
    <span className="coordinate-number" aria-label={String(value)} style={{ width: `${width}ch` }}>
      <i
        key={transition.revision}
        className={`coordinate-roll${increasing ? ' increasing' : decreasing ? ' decreasing' : ''}`}
        style={{
          '--coordinate-steps': steps,
          '--coordinate-offset': `${-steps}em`,
          animationDuration: `${Math.min(650, 180 + steps * 32)}ms`,
        } as CSSProperties}
        aria-hidden="true"
      >
        {sequence.map((number) => <b key={number}>{number}</b>)}
      </i>
    </span>
  );
}

function CasinoCoordinates({ x, y }: { x: number; y: number }) {
  return <span className="casino-coordinates"><RollingCoordinate value={x} /><span>,</span><RollingCoordinate value={y} /></span>;
}

export function BattleScreen({ online }: { online: number | null }) {
  const [placementCooldownMs, setPlacementCooldownMs] = useState(5000);
  const [paused, setPaused] = useState(false);
  const [zoom, setZoom] = useState(10);
  const zoomRef = useRef(10);
  const zoomAnimationRef = useRef<number | null>(null);
  const [color, setColor] = useState<string>(getSavedColor);
  const [hexInput, setHexInput] = useState(color);
  const [selectedPixel, setSelectedPixel] = useState<{ x: number; y: number } | null>(null);
  const [paintNonce, setPaintNonce] = useState(0);
  const [cooldownUntil, setCooldownUntil] = useState(0);
  const rateLimitedUntilRef = useRef(0);
  const [cooldownSeconds, setCooldownSeconds] = useState(0);
  const [inspectedPixel, setInspectedPixel] = useState<Pixel | null>(null);
  const [paletteOpen, setPaletteOpen] = useState(localPalettePreview);
  const [infoOpen, setInfoOpen] = useState(false);
  const [copied, setCopied] = useState(false);
  const [hexCopied, setHexCopied] = useState(false);
  const [templateImageUrl, setTemplateImageUrl] = useState<string | null>(null);
  const [templatePlacement, setTemplatePlacement] = useState<TemplatePlacement | null>(null);
  const [inventory, setInventory] = useState<Inventory>(EMPTY_INVENTORY);
  const [itemBusy, setItemBusy] = useState<'bomb' | 'ice' | null>(null);
  const [itemMode, setItemMode] = useState<'bomb' | 'ice' | null>(null);
  const [clock, setClock] = useState(() => Date.now());
  const templateInputRef = useRef<HTMLInputElement>(null);
  const templateSelectionRevisionRef = useRef(0);
  const currentUserID = String(window.Telegram?.WebApp?.initDataUnsafe?.user?.id ?? '');
  const frozenUntil = inspectedPixel?.frozenUntil ? Date.parse(inspectedPixel.frozenUntil) : 0;
  const frozenSeconds = Math.max(0, Math.ceil((frozenUntil - clock) / 1000));
  const isForeignFrozen = frozenSeconds > 0 && Boolean(inspectedPixel?.author?.id) && inspectedPixel?.author?.id !== currentUserID;
  const frozenLabel = frozenSeconds >= 60 ? `Лёд: ${Math.ceil(frozenSeconds / 60)} мин` : `Лёд: ${frozenSeconds} сек`;

  useEffect(() => {
    try {
      window.localStorage.setItem(SELECTED_COLOR_STORAGE_KEY, color);
    } catch {
      // Keep the selected color for the current session if storage is unavailable.
    }
  }, [color]);

  useEffect(() => setHexInput(color), [color]);

  const inspectPixel = useCallback((pixel: Pixel | null) => {
    const pixelAuthor = pixel?.author;
    if (!pixelAuthor?.id) {
      setInspectedPixel(pixel);
      return;
    }
    if (pixelAuthor.displayName || pixelAuthor.username) {
      profileCache.set(pixelAuthor.id, { profile: pixelAuthor, expiresAt: Date.now() + PROFILE_CACHE_MS });
      setInspectedPixel(pixel);
      return;
    }
    const cached = profileCache.get(pixelAuthor.id);
    if (cached && cached.expiresAt > Date.now()) {
      setInspectedPixel(pixel ? { ...pixel, author: cached.profile } : pixel);
      return;
    }
    profileCache.delete(pixelAuthor.id);
    setInspectedPixel(pixel);
  }, []);

  const setZoomImmediately = useCallback((value: number) => {
    if (zoomAnimationRef.current !== null) {
      window.cancelAnimationFrame(zoomAnimationRef.current);
      zoomAnimationRef.current = null;
    }
    zoomRef.current = value;
    setZoom(value);
  }, []);

  const setZoomSmoothly = useCallback((target: number) => {
    const clampedTarget = Math.min(100, Math.max(0.5, target));
    if (zoomAnimationRef.current !== null) window.cancelAnimationFrame(zoomAnimationRef.current);
    const animate = () => {
      const current = zoomRef.current;
      const difference = clampedTarget - current;
      const next = Math.abs(difference) < Math.max(0.002, clampedTarget * 0.001)
        ? clampedTarget
        : current + difference * 0.24;
      zoomRef.current = next;
      setZoom(next);
      if (next === clampedTarget) {
        zoomAnimationRef.current = null;
        return;
      }
      zoomAnimationRef.current = window.requestAnimationFrame(animate);
    };
    zoomAnimationRef.current = window.requestAnimationFrame(animate);
  }, []);

  useEffect(() => () => {
    if (zoomAnimationRef.current !== null) window.cancelAnimationFrame(zoomAnimationRef.current);
  }, []);

  useEffect(() => () => {
    if (templateImageUrl) URL.revokeObjectURL(templateImageUrl);
  }, [templateImageUrl]);

  useEffect(() => {
    if (!currentUserID) return;
    let disposed = false;
    const selectionRevision = templateSelectionRevisionRef.current;
    void loadTemplate(currentUserID).then((stored) => {
      if (disposed || selectionRevision !== templateSelectionRevisionRef.current || !stored?.image) return;
      setTemplatePlacement(stored.placement ?? null);
      setTemplateImageUrl(URL.createObjectURL(stored.image));
    }).catch(() => undefined);
    return () => { disposed = true; };
  }, [currentUserID]);

  const persistTemplatePlacement = useCallback((placement: TemplatePlacement) => {
    setTemplatePlacement(placement);
    if (currentUserID) void saveTemplatePlacement(currentUserID, placement).catch(() => undefined);
  }, [currentUserID]);

  useEffect(() => {
    if (!cooldownUntil) return;
    const update = () => {
      const seconds = Math.max(0, Math.ceil((cooldownUntil - Date.now()) / 1000));
      setCooldownSeconds(seconds);
      if (!seconds) setCooldownUntil(0);
    };
    update();
    const timer = window.setInterval(update, 250);
    return () => window.clearInterval(timer);
  }, [cooldownUntil]);

  useEffect(() => {
    const handleRateLimit = (event: Event) => {
      const retryAfterMs = Math.max(1000, Number((event as CustomEvent<{ retryAfterMs?: number }>).detail?.retryAfterMs) || 1000);
      rateLimitedUntilRef.current = Date.now() + retryAfterMs;
      setCooldownUntil((current) => Math.max(current, rateLimitedUntilRef.current));
    };
    window.addEventListener('pixelbattle:rate-limited', handleRateLimit);
    return () => window.removeEventListener('pixelbattle:rate-limited', handleRateLimit);
  }, []);

  useEffect(() => {
    const frozenUntil = inspectedPixel?.frozenUntil ? Date.parse(inspectedPixel.frozenUntil) : 0;
    if (!frozenUntil || frozenUntil <= Date.now()) return;
    const timer = window.setInterval(() => setClock(Date.now()), 250);
    return () => window.clearInterval(timer);
  }, [inspectedPixel?.frozenUntil]);

  useEffect(() => {
    const initData = window.Telegram?.WebApp?.initData;
    if (!initData) return;
    const apiUrl = import.meta.env.VITE_API_URL ?? window.location.origin;
    let disposed = false;
    const refreshPolicy = () => fetch(`${apiUrl}/api/boards/session`, { cache: 'no-store', headers: { 'X-Telegram-Init-Data': initData } })
      .then((response) => response.ok ? response.json() as Promise<{ cooldownBypassed: boolean; cooldownMs?: number; paused?: boolean; inventory?: Inventory }> : null)
      .then((result) => {
        if (!disposed && result) {
          const nextCooldown = Math.max(0, result.cooldownMs ?? (result.cooldownBypassed ? 0 : 5000));
          setPlacementCooldownMs(nextCooldown);
          setPaused(Boolean(result.paused));
          if (result.inventory) setInventory(result.inventory);
          if (!nextCooldown && Date.now() >= rateLimitedUntilRef.current) { setCooldownUntil(0); setCooldownSeconds(0); }
        }
      })
      .catch(() => undefined);
    void refreshPolicy();
    const timer = window.setInterval(refreshPolicy, 2000);
    return () => { disposed = true; window.clearInterval(timer); };
  }, []);

  useEffect(() => {
    if (!infoOpen) return;
    const close = (event: KeyboardEvent) => { if (event.key === 'Escape') setInfoOpen(false); };
    window.addEventListener('keydown', close);
    return () => window.removeEventListener('keydown', close);
  }, [infoOpen]);

  useEffect(() => {
    const author = inspectedPixel?.author;
    if (!author?.id || author.displayName) return;
    const cached = profileCache.get(author.id);
    if (cached && cached.expiresAt > Date.now()) {
      setInspectedPixel((pixel) => pixel?.author?.id === author.id ? { ...pixel, author: cached.profile } : pixel);
      return;
    }
    profileCache.delete(author.id);
    const initData = window.Telegram?.WebApp?.initData;
    if (!initData) return;
    const apiUrl = import.meta.env.VITE_API_URL ?? window.location.origin;
    let active = true;
    let request = profileRequests.get(author.id);
    if (!request) {
      request = fetch(`${apiUrl}/api/boards/profiles/${encodeURIComponent(author.id)}`, {
        cache: 'no-store',
        headers: { 'X-Telegram-Init-Data': initData },
      })
        .then((response) => response.ok ? response.json() as Promise<Omit<PixelAuthor, 'id'>> : null)
        .then((profile) => {
          if (!profile) return null;
          const publicProfile: PixelAuthor = { ...profile, id: author.id };
          profileCache.set(author.id, { profile: publicProfile, expiresAt: Date.now() + PROFILE_CACHE_MS });
          return publicProfile;
        })
        .catch((error: unknown) => {
          console.error('Failed to preload profile', error);
          return null;
        })
        .finally(() => profileRequests.delete(author.id));
      profileRequests.set(author.id, request);
    }
    void request
      .then((profile) => {
        if (!active || !profile) return;
        setInspectedPixel((pixel) => pixel?.author?.id === author.id ? { ...pixel, author: profile } : pixel);
      });
    return () => { active = false; };
  }, [inspectedPixel?.author?.id, inspectedPixel?.author?.displayName]);

  const copyCoordinates = async () => {
    const coordinates = selectedPixel ?? (localPalettePreview ? { x: 0, y: 0 } : null);
    if (!coordinates || !await writeClipboardText(`${coordinates.x},${coordinates.y}`)) return;
    setCopied(true);
    window.setTimeout(() => setCopied(false), 1200);
  };

  const copyHex = async () => {
    if (!await writeClipboardText(color.toUpperCase())) return;
    setHexCopied(true);
    window.setTimeout(() => setHexCopied(false), 1200);
  };

  const updateHexInput = (value: string) => {
    const nextValue = value.trim().toUpperCase();
    setHexInput(nextValue);
    const match = nextValue.match(/^#?([0-9A-F]{6})$/);
    if (match) setColor(`#${match[1]}`);
  };

  const author = inspectedPixel?.author;
  const visibleAuthor = author && (author.username || author.displayName) ? author : null;
  const safePhotoUrl = visibleAuthor?.photoUrl?.startsWith('https://') ? visibleAuthor.photoUrl : undefined;
  const safeUsername = visibleAuthor?.username && /^[A-Za-z0-9_]{1,32}$/.test(visibleAuthor.username) ? visibleAuthor.username : undefined;
  const authorLabel = visibleAuthor?.username ? `@${visibleAuthor.username}` : visibleAuthor?.displayName ?? '';
  const authorTitle = visibleAuthor?.displayName || authorLabel;
  const authorIdentity = authorLabel || 'Нет данных';
  const contrastClass = '';

  const openInfo = () => {
    if (!author) return;
    setInfoOpen(true);
  };

  const activateIce = async () => {
    if (inventory.freezeRemaining > 0) return true;
    if (itemBusy || inventory.ice <= 0) return false;
    const initData = window.Telegram?.WebApp?.initData;
    if (!initData) return false;
    setItemBusy('ice');
    try {
      const apiUrl = import.meta.env.VITE_API_URL ?? window.location.origin;
      const response = await fetch(`${apiUrl}/api/boards/items/ice/activate`, { method: 'POST', cache: 'no-store', headers: { 'X-Telegram-Init-Data': initData } });
      const result = await response.json() as { activated?: boolean; inventory?: Inventory };
      if (result.inventory) setInventory(result.inventory);
      return Boolean(result.activated || (result.inventory?.freezeRemaining ?? 0) > 0);
    } catch { /* Session refresh will restore the inventory. */ }
    finally { setItemBusy(null); }
    return false;
  };

  const explodeBomb = async () => {
    if (itemBusy || paused || !selectedPixel || inventory.bombs <= 0 || cooldownSeconds > 0) return;
    const initData = window.Telegram?.WebApp?.initData;
    if (!initData) return;
    setItemBusy('bomb');
    try {
      const apiUrl = import.meta.env.VITE_API_URL ?? window.location.origin;
      const response = await fetch(`${apiUrl}/api/boards/items/bomb/use`, {
        method: 'POST', cache: 'no-store',
        headers: { 'Content-Type': 'application/json', 'X-Telegram-Init-Data': initData },
        body: JSON.stringify({ ...selectedPixel, color, operationId: crypto.randomUUID() }),
      });
      if (response.status === 429) {
        const retryAfter = Math.max(1, Number(response.headers.get('Retry-After')) || Math.ceil(placementCooldownMs / 1000));
        rateLimitedUntilRef.current = Date.now() + retryAfter * 1000;
        setCooldownUntil(rateLimitedUntilRef.current);
        return;
      }
      const result = await response.json() as { inventory?: Inventory; cooldownMs?: number; pixels?: Pixel[] };
      if (result.inventory) setInventory(result.inventory);
      if (response.ok) {
        if (result.pixels?.length) {
          window.dispatchEvent(new CustomEvent('pixelbattle:pixels-applied', { detail: { pixels: result.pixels } }));
        }
        setItemMode(null);
        const cooldownMs = Math.max(0, result.cooldownMs ?? placementCooldownMs);
        if (cooldownMs > 0) setCooldownUntil(Date.now() + cooldownMs);
        window.dispatchEvent(new Event('pixelbattle:placement-accepted'));
      }
    } catch { /* Session refresh will restore the inventory. */ }
    finally { setItemBusy(null); }
  };

  const performAction = async () => {
    if (itemBusy || paused || !selectedPixel || isForeignFrozen || cooldownSeconds > 0) return;
    if (itemMode === 'bomb') {
      await explodeBomb();
      return;
    }
    if (itemMode === 'ice' && !(await activateIce())) return;
    setPaintNonce((value) => value + 1);
  };

  useEffect(() => {
    const handleEnter = (event: KeyboardEvent) => {
      if (event.key !== 'Enter' || event.repeat || infoOpen) return;
      const target = event.target;
      if (target instanceof HTMLElement && target.matches('button, input, textarea, select')) return;
      event.preventDefault();
      void performAction();
    };
    window.addEventListener('keydown', handleEnter);
    return () => window.removeEventListener('keydown', handleEnter);
  });

  const paintLabel = paused
    ? 'Игра на паузе'
    : isForeignFrozen
      ? frozenLabel
      : cooldownSeconds > 0
      ? `Через ${cooldownSeconds} с`
      : itemMode === 'bomb'
        ? 'Взорвать'
      : itemMode === 'ice'
        ? 'Заморозить'
      : 'Покрасить';

  return (
    <div className="battle-screen">
      <SeasonStatus online={online} showOnline />
      <PixelBoard
        color={color}
        zoom={zoom}
        onZoom={setZoomImmediately}
        eyedropper={false}
        onPickColor={setColor}
        onEyedropperEnd={() => undefined}
        paintNonce={paintNonce}
        useIce={itemMode === 'ice'}
        onSelectPixel={(pixel) => {
          setSelectedPixel(pixel);
          if (!pixel) {
            setInspectedPixel(null);
            setInfoOpen(false);
          }
        }}
        onInspectPixel={inspectPixel}
        cooldownUntil={cooldownUntil}
        onPlacementAccepted={(serverCooldownMs) => {
          const cooldownMs = Math.max(0, serverCooldownMs ?? placementCooldownMs);
          if (cooldownMs > 0) setCooldownUntil(Date.now() + cooldownMs);
          if (itemMode === 'ice') {
            const nextRemaining = Math.max(0, inventory.freezeRemaining - 1);
            setInventory((current) => ({ ...current, freezeRemaining: nextRemaining }));
            if (nextRemaining === 0) setItemMode(null);
          }
          window.dispatchEvent(new Event('pixelbattle:placement-accepted'));
        }}
        templateImageUrl={templateImageUrl}
        templatePlacement={templatePlacement}
        templateOpacity={getTemplateOpacity() / 100}
        onTemplatePlacementChange={persistTemplatePlacement}
      />
      <GlassControls
        zoom={zoom}
        onZoom={setZoomSmoothly}
        onImageTemplate={() => templateInputRef.current?.click()}
        hasImageTemplate={Boolean(templateImageUrl)}
        onCancelImageTemplate={() => {
          templateSelectionRevisionRef.current += 1;
          setTemplateImageUrl(null);
          setTemplatePlacement(null);
          if (currentUserID) void removeTemplate(currentUserID).catch(() => undefined);
        }}
      />
      <input
        ref={templateInputRef}
        type="file"
        accept="image/png,image/jpeg,image/webp,image/gif"
        hidden
        onChange={(event) => {
          const file = event.target.files?.[0];
          event.target.value = '';
          if (!file || !file.type.startsWith('image/') || file.size > 20 * 1024 * 1024) return;
          templateSelectionRevisionRef.current += 1;
          setTemplatePlacement(null);
          setTemplateImageUrl(URL.createObjectURL(file));
          if (currentUserID) void saveTemplateImage(currentUserID, file).catch(() => undefined);
        }}
      />

      <section className="placement-dock" aria-label="Панель закрашивания">
        <div className={`placement-meta-row${selectedPixel && visibleAuthor ? '' : ' without-owner'}`}>
            {selectedPixel && visibleAuthor && <div className="pixel-owner-card" key={visibleAuthor.id}>
              {safePhotoUrl
                 ? <img className="pixel-owner-avatar" src={safePhotoUrl} alt="" />
                : <span className="pixel-owner-avatar pixel-owner-fallback">{authorLabel.slice(0, 1).toUpperCase()}</span>}
              {safeUsername
                ? <a className="pixel-owner-name" href={`https://t.me/${safeUsername}`} target="_blank" rel="noreferrer" onClick={(event) => openTelegramProfile(event, safeUsername)} aria-label={`Открыть профиль ${authorLabel}`}>{authorLabel}</a>
                : <span className="pixel-owner-name">{authorLabel}</span>}
              <span className="pixel-owner-balance" aria-hidden="true" />
            </div>}
            <button className={`pixel-item-button${itemMode === 'bomb' ? ' selected' : ''}`} onClick={() => setItemMode((current) => current === 'bomb' ? null : 'bomb')} disabled={itemBusy !== null || paused || inventory.bombs <= 0 || cooldownSeconds > 0} aria-label="Выбрать бомбу" aria-pressed={itemMode === 'bomb'}>
              <img src="/assets/bomb.svg" alt="" /><span>{inventory.bombs}</span>
            </button>
            <button className={`pixel-item-button${itemMode === 'ice' ? ' selected' : ''}`} onClick={() => setItemMode((current) => current === 'ice' ? null : 'ice')} disabled={itemBusy !== null || paused || (inventory.ice <= 0 && inventory.freezeRemaining <= 0) || cooldownSeconds > 0} aria-label="Выбрать заморозку" aria-pressed={itemMode === 'ice'}>
              <img src="/assets/ice.svg" alt="" /><span>{inventory.ice}</span>
            </button>
          </div>
        <div className={`placement-frame${paletteOpen ? ' open' : ''}`}>
          <button className="selected-color" style={{ '--selected-color': color } as CSSProperties} onClick={() => setPaletteOpen((open) => !open)} aria-label="Открыть палитру" aria-expanded={paletteOpen} />
          <button className={`selected-color-hex${hexCopied ? ' copied' : ''}`} onClick={() => void copyHex()} aria-label={`Скопировать HEX ${color.toUpperCase()}`}>{color.toUpperCase()}</button>
          <button className={`coordinate-copy${copied ? ' copied' : ''}${contrastClass}`} onClick={() => void copyCoordinates()} disabled={!selectedPixel && !localPalettePreview} aria-label="Скопировать координаты">
            <CasinoCoordinates x={selectedPixel?.x ?? 0} y={selectedPixel?.y ?? 0} />
          </button>
          <div className="placement-palette" aria-label="Палитра цветов" aria-hidden={!paletteOpen}>
            {COLORS.map((value) => (
              <button
                key={value}
                className={color.toUpperCase() === value ? 'selected' : ''}
                style={{ backgroundColor: value, borderColor: value === '#FDFDFD' ? '#DBDBDB' : 'transparent' }}
                onClick={() => { setColor(value); setPaletteOpen(false); }}
                aria-label={`Выбрать цвет ${value}`}
                aria-pressed={color.toUpperCase() === value}
                tabIndex={paletteOpen ? 0 : -1}
              />
            ))}
            <input
              className="color-spectrum"
              type="range"
              min="0"
              max="359"
              value={hexToHue(color)}
              onChange={(event) => setColor(pigmentToHex(
                Number(event.currentTarget.value),
                hexToPigment(color),
              ))}
              aria-label="Выбрать цвет на спектре"
              tabIndex={paletteOpen ? 0 : -1}
              style={{ '--slider-color': hueToHex(hexToHue(color)) } as CSSProperties}
            />
            <input
              className="color-pigment color-slider"
              type="range"
              min="0"
              max="100"
              value={hexToPigment(color)}
              onChange={(event) => setColor(pigmentToHex(
                hexToHue(color),
                Number(event.currentTarget.value),
              ))}
              aria-label="Добавить белый или чёрный пигмент"
              tabIndex={paletteOpen ? 0 : -1}
              style={{ '--hue-color': hueToHex(hexToHue(color)), '--slider-color': color } as CSSProperties}
            />
            <input
              className="color-hex-input"
              type="text"
              value={hexInput}
              maxLength={7}
              placeholder="#RRGGBB"
              onChange={(event) => updateHexInput(event.currentTarget.value)}
              onBlur={() => setHexInput(color)}
              onKeyDown={(event) => {
                if (event.key === 'Enter') event.currentTarget.blur();
              }}
              aria-label="Введите HEX-код цвета"
              autoComplete="off"
              spellCheck={false}
              tabIndex={paletteOpen ? 0 : -1}
            />
          </div>
        </div>

        <button
          className={`paint-action visible${isForeignFrozen ? ' frozen' : ''}`}
          onClick={() => void performAction()}
          disabled={paused || !selectedPixel || itemBusy !== null || isForeignFrozen || cooldownSeconds > 0}
          title={`${itemMode === 'bomb' ? 'Взорвать' : itemMode === 'ice' ? 'Заморозить' : 'Закрасить'} (Enter)`}
          aria-disabled={paused || !selectedPixel || itemBusy !== null || isForeignFrozen || cooldownSeconds > 0}
        >
          {paintLabel}
        </button>
      </section>

      {infoOpen && visibleAuthor && (
        <div className="profile-modal-backdrop" role="presentation" onPointerDown={(event) => { if (event.target === event.currentTarget) setInfoOpen(false); }}>
          <section className="profile-modal glass-panel" role="dialog" aria-modal="true" aria-label="Информация об игроке">
            <button className="profile-modal-close" onClick={() => setInfoOpen(false)} aria-label="Закрыть">×</button>
             {safePhotoUrl
               ? <img className="profile-modal-avatar" src={safePhotoUrl} alt="" />
              : <span className="profile-modal-avatar profile-modal-fallback">{authorTitle.slice(0, 1).toUpperCase()}</span>}
            <div className="profile-modal-identity">
              <strong>{authorTitle}</strong>
               {safeUsername
                 ? <a href={`https://t.me/${safeUsername}`} target="_blank" rel="noreferrer" onClick={(event) => openTelegramProfile(event, safeUsername)}>{authorIdentity}</a>
                : <span>{authorIdentity}</span>}
            </div>
          </section>
        </div>
      )}
    </div>
  );
}

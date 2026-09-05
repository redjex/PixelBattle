import { useCallback, useEffect, useLayoutEffect, useRef, useState, type CSSProperties } from 'react';
import { GlassControls } from './GlassControls';
import { PixelBoard } from './PixelBoard';
import type { Pixel } from '../types/pixel';

const COLORS = [
  '#FF8080', '#FFCA73', '#FBFFA5', '#7CFF80', '#7EFFF2', '#84D0FF', '#8290FF', '#CD81FF', '#FF80D0', '#FDFDFD',
  '#FF0000', '#FF9D00', '#F2FF00', '#00FF07', '#00FFE6', '#009DFF', '#001EFF', '#9900FF', '#FF00A1', '#8A8A8A',
  '#870000', '#8D4E00', '#B6A700', '#009904', '#009687', '#00568C', '#001194', '#53008A', '#8E005A', '#000000',
] as const;

type PixelAuthor = NonNullable<Pixel['author']>;
type Inventory = { bombs: number; ice: number; freezeRemaining: number };
const profileCache = new Map<string, PixelAuthor>();
const profileRequests = new Map<string, Promise<PixelAuthor | null>>();
const EMPTY_INVENTORY: Inventory = { bombs: 0, ice: 0, freezeRemaining: 0 };

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

export function BattleScreen() {
  const [placementCooldownMs, setPlacementCooldownMs] = useState(5000);
  const [paused, setPaused] = useState(false);
  const [zoom, setZoom] = useState(10);
  const zoomRef = useRef(10);
  const zoomAnimationRef = useRef<number | null>(null);
  const [color, setColor] = useState<string>('#009DFF');
  const [selectedPixel, setSelectedPixel] = useState<{ x: number; y: number } | null>(null);
  const [paintNonce, setPaintNonce] = useState(0);
  const [cooldownUntil, setCooldownUntil] = useState(0);
  const [cooldownSeconds, setCooldownSeconds] = useState(0);
  const [inspectedPixel, setInspectedPixel] = useState<Pixel | null>(null);
  const [paletteOpen, setPaletteOpen] = useState(false);
  const [infoOpen, setInfoOpen] = useState(false);
  const [copied, setCopied] = useState(false);
  const [templateImageUrl, setTemplateImageUrl] = useState<string | null>(null);
  const [inventory, setInventory] = useState<Inventory>(EMPTY_INVENTORY);
  const [itemBusy, setItemBusy] = useState<'bomb' | 'ice' | null>(null);
  const [itemMode, setItemMode] = useState<'bomb' | 'ice' | null>(null);
  const [clock, setClock] = useState(() => Date.now());
  const templateInputRef = useRef<HTMLInputElement>(null);
  const currentUserID = String(window.Telegram?.WebApp?.initDataUnsafe?.user?.id ?? '');
  const frozenUntil = inspectedPixel?.frozenUntil ? Date.parse(inspectedPixel.frozenUntil) : 0;
  const frozenSeconds = Math.max(0, Math.ceil((frozenUntil - clock) / 1000));
  const isForeignFrozen = frozenSeconds > 0 && Boolean(inspectedPixel?.author?.id) && inspectedPixel?.author?.id !== currentUserID;
  const frozenLabel = frozenSeconds >= 60 ? `Лёд: ${Math.ceil(frozenSeconds / 60)} мин` : `Лёд: ${frozenSeconds} сек`;

  const inspectPixel = useCallback((pixel: Pixel | null) => {
    const pixelAuthor = pixel?.author;
    if (!pixelAuthor?.id) {
      setInspectedPixel(pixel);
      return;
    }
    if (pixelAuthor.displayName || pixelAuthor.username) {
      profileCache.set(pixelAuthor.id, pixelAuthor);
      setInspectedPixel(pixel);
      return;
    }
    const cached = profileCache.get(pixelAuthor.id);
    setInspectedPixel(cached && pixel ? { ...pixel, author: cached } : pixel);
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
        : current + difference * 0.2;
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
          if (!nextCooldown) { setCooldownUntil(0); setCooldownSeconds(0); }
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
    if (cached) {
      setInspectedPixel((pixel) => pixel?.author?.id === author.id ? { ...pixel, author: cached } : pixel);
      return;
    }
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
        .then((response) => response.ok ? response.json() as Promise<PixelAuthor> : null)
        .then((profile) => {
          if (profile) profileCache.set(author.id, profile);
          return profile;
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
    if (!selectedPixel) return;
    await navigator.clipboard.writeText(`${selectedPixel.x},${selectedPixel.y}`);
    setCopied(true);
    window.setTimeout(() => setCopied(false), 1200);
  };

  const author = inspectedPixel?.author;
  const visibleAuthor = author && (author.username || author.displayName) ? author : null;
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
    if (itemBusy || paused || !selectedPixel || inventory.bombs <= 0) return;
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
      const result = await response.json() as { inventory?: Inventory };
      if (result.inventory) setInventory(result.inventory);
      if (response.ok) {
        setItemMode(null);
        window.dispatchEvent(new Event('pixelbattle:placement-accepted'));
      }
    } catch { /* Session refresh will restore the inventory. */ }
    finally { setItemBusy(null); }
  };

  const performAction = async () => {
    if (itemBusy || paused || !selectedPixel || isForeignFrozen || (cooldownSeconds > 0 && itemMode !== 'bomb')) return;
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
      : itemMode === 'bomb'
        ? 'Взорвать'
      : cooldownSeconds > 0
      ? `Через ${cooldownSeconds} с`
      : itemMode === 'ice'
        ? 'Заморозить'
      : 'Покрасить';

  return (
    <div className="battle-screen">
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
        onPlacementAccepted={() => {
          if (placementCooldownMs > 0) setCooldownUntil(Date.now() + placementCooldownMs);
          if (itemMode === 'ice') {
            const nextRemaining = Math.max(0, inventory.freezeRemaining - 1);
            setInventory((current) => ({ ...current, freezeRemaining: nextRemaining }));
            if (nextRemaining === 0) setItemMode(null);
          }
          window.dispatchEvent(new Event('pixelbattle:placement-accepted'));
        }}
        templateImageUrl={templateImageUrl}
      />
      <GlassControls
        zoom={zoom}
        onZoom={setZoomSmoothly}
        onImageTemplate={() => templateInputRef.current?.click()}
        hasImageTemplate={Boolean(templateImageUrl)}
        onCancelImageTemplate={() => {
          setTemplateImageUrl(null);
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
          setTemplateImageUrl(URL.createObjectURL(file));
        }}
      />

      <section className="placement-dock" aria-label="Панель закрашивания">
        <div className={`placement-meta-row${selectedPixel && visibleAuthor ? '' : ' without-owner'}`}>
            {selectedPixel && visibleAuthor && <div className="pixel-owner-card" key={visibleAuthor.id}>
              {visibleAuthor.photoUrl
                ? <img className="pixel-owner-avatar" src={visibleAuthor.photoUrl} alt="" />
                : <span className="pixel-owner-avatar pixel-owner-fallback">{authorLabel.slice(0, 1).toUpperCase()}</span>}
              <span className="pixel-owner-name">{authorLabel}</span>
              <span className="pixel-owner-balance" aria-hidden="true" />
            </div>}
            <button className={`pixel-item-button${itemMode === 'bomb' ? ' selected' : ''}`} onClick={() => setItemMode((current) => current === 'bomb' ? null : 'bomb')} disabled={itemBusy !== null || paused || inventory.bombs <= 0} aria-label="Выбрать бомбу" aria-pressed={itemMode === 'bomb'}>
              <img src="/assets/bomb.svg" alt="" /><span>{inventory.bombs}</span>
            </button>
            <button className={`pixel-item-button${itemMode === 'ice' ? ' selected' : ''}`} onClick={() => setItemMode((current) => current === 'ice' ? null : 'ice')} disabled={itemBusy !== null || (inventory.ice <= 0 && inventory.freezeRemaining <= 0)} aria-label="Выбрать заморозку" aria-pressed={itemMode === 'ice'}>
              <img src="/assets/ice.svg" alt="" /><span>{inventory.ice}</span>
            </button>
          </div>
        <div className={`placement-frame${paletteOpen ? ' open' : ''}`}>
          <button className="selected-color" style={{ backgroundColor: color }} onClick={() => setPaletteOpen((open) => !open)} aria-label="Открыть палитру" aria-expanded={paletteOpen} />
          <button className={`coordinate-copy${copied ? ' copied' : ''}${contrastClass}`} onClick={copyCoordinates} disabled={!selectedPixel} aria-label="Скопировать координаты">
            <CasinoCoordinates x={selectedPixel?.x ?? 0} y={selectedPixel?.y ?? 0} />
          </button>
          <span className="placement-spacer" aria-hidden="true" />
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
          </div>
        </div>

        <button
          className={`paint-action visible${isForeignFrozen ? ' frozen' : ''}`}
          onClick={() => void performAction()}
          disabled={paused || !selectedPixel || itemBusy !== null || isForeignFrozen || (cooldownSeconds > 0 && itemMode !== 'bomb')}
          title={`${itemMode === 'bomb' ? 'Взорвать' : itemMode === 'ice' ? 'Заморозить' : 'Закрасить'} (Enter)`}
          aria-disabled={paused || !selectedPixel || itemBusy !== null || isForeignFrozen || (cooldownSeconds > 0 && itemMode !== 'bomb')}
        >
          {paintLabel}
        </button>
      </section>

      {infoOpen && visibleAuthor && (
        <div className="profile-modal-backdrop" role="presentation" onPointerDown={(event) => { if (event.target === event.currentTarget) setInfoOpen(false); }}>
          <section className="profile-modal glass-panel" role="dialog" aria-modal="true" aria-label="Информация об игроке">
            <button className="profile-modal-close" onClick={() => setInfoOpen(false)} aria-label="Закрыть">×</button>
            {visibleAuthor.photoUrl
              ? <img className="profile-modal-avatar" src={visibleAuthor.photoUrl} alt="" />
              : <span className="profile-modal-avatar profile-modal-fallback">{authorTitle.slice(0, 1).toUpperCase()}</span>}
            <div className="profile-modal-identity">
              <strong>{authorTitle}</strong>
              {visibleAuthor.username
                ? <a href={`https://t.me/${visibleAuthor.username}`} target="_blank" rel="noreferrer">{authorIdentity}</a>
                : <span>{authorIdentity}</span>}
            </div>
          </section>
        </div>
      )}
    </div>
  );
}

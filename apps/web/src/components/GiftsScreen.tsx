import { useEffect, useMemo, useRef, useState } from 'react';
import { TgsPlayer } from './TgsPlayer';
import type { TrophyItemReward } from '../telegram';

type Props = {
  onBack: () => void;
  onOpenCatalog: () => void;
  prizes: unknown[];
  pendingItemRewards: TrophyItemReward[];
  onItemRewardClaimed: (rewardId: number) => void;
  focusRewardId?: string | null;
  onRewardOpened?: () => void;
};
type CatalogProps = { onBack: () => void; prizes: unknown[]; soldOutTrophies: string[] };

type TrophyReward = {
  trophyId: string;
  kind: 'code' | 'url';
  value: string;
};

const trophyRewardCache = new Map<string, TrophyReward>();

type TrophyPart = {
  src: string;
  className: string;
  number: number;
};

type TrophyDefinition = {
  id: string;
  aliases: string[];
  name: string;
  total: number;
  base: string;
  preview?: string;
  puzzleImage?: string;
  icon?: string;
  parts: TrophyPart[];
};

type TrophyRarity = 'legendary' | 'rare' | 'uncommon' | 'common';

const TROPHIES: TrophyDefinition[] = [
  {
    id: 'experience', aliases: ['xp', 'experience', 'опыт'], name: 'Опыт +100', total: 1,
    base: '', icon: '/assets/exp.svg?v=2', parts: [],
  },
  {
    id: 'bomb', aliases: ['bomb', 'бомба'], name: 'Бомбы ×5', total: 1,
    base: '', icon: '/assets/bomb.svg', parts: [],
  },
  {
    id: 'ice', aliases: ['ice', 'freeze', 'заморозка'], name: 'Заморозки ×5', total: 1,
    base: '', icon: '/assets/ice.svg', parts: [],
  },
  {
    id: 'yng-explrz', aliases: ['yng explrz', 'yng_explrz'], name: 'YNG EXPLRZ', total: 2,
    base: '/assets/trophies/yng-explrz-base.svg',
    preview: '/assets/trophies/yng-explrz-main.svg?v=4',
    parts: [
      { src: '/assets/trophies/yng-explrz-1v2.svg?v=3', className: 'trophy-part-half-right', number: 1 },
      { src: '/assets/trophies/yng-explrz-2v2.svg?v=3', className: 'trophy-part-half-left', number: 2 },
    ],
  },
  {
    id: 'besigned', aliases: ['be signed', 'be_signed'], name: 'BeSigned', total: 2,
    base: '/assets/trophies/besigned-base.svg',
    preview: '/assets/trophies/besigned-main.svg?v=4',
    parts: [
      { src: '/assets/trophies/besigned-1v2.svg?v=3', className: 'trophy-part-half-right', number: 1 },
      { src: '/assets/trophies/besigned-2v2.svg?v=3', className: 'trophy-part-half-left', number: 2 },
    ],
  },
  {
    id: 'stickers', aliases: ['stickers', 'стикеры'], name: 'Стикеры', total: 2,
    base: '/assets/trophies/two-part-base.svg',
    preview: '/assets/trophies/stickers-main.svg?v=4',
    parts: [
      { src: '/assets/trophies/stickers-1v2.svg?v=3', className: 'trophy-part-half-right', number: 1 },
      { src: '/assets/trophies/stickers-2v2.svg?v=3', className: 'trophy-part-half-left', number: 2 },
    ],
  },
  {
    id: 'stikidbot', aliases: ['stikidbot', 'stik id bot'], name: 'StikIdBot', total: 2,
    base: '', preview: '/assets/trophies/mars.png?v=1',
    puzzleImage: '/assets/trophies/mars.png?v=1', parts: [],
  },
  {
    id: 'stashvpn', aliases: ['stashvpn', 'stash vpn'], name: 'StashVPN', total: 2,
    base: '', preview: '/assets/trophies/stash.png?v=1',
    puzzleImage: '/assets/trophies/stash.png?v=1', parts: [],
  },
  {
    id: 'bear', aliases: ['bear', 'мишка'], name: 'Мишка', total: 2,
    base: '/assets/trophies/two-part-base.svg',
    preview: '/assets/trophies/bear-main.svg?v=4',
    parts: [
      { src: '/assets/trophies/bear-1v2.svg?v=3', className: 'trophy-part-half-right', number: 1 },
      { src: '/assets/trophies/bear-2v2.svg?v=3', className: 'trophy-part-half-left', number: 2 },
    ],
  },
  {
    id: 'bear-redjex', aliases: ['bear redjex', 'мишка redjex', 'мишка от redjex'],
    name: 'Мишка от redjex', total: 2, base: '',
    preview: '/assets/trophies/bear-redjex.png?v=1',
    puzzleImage: '/assets/trophies/bear-redjex.png?v=1', parts: [],
  },
  {
    id: 'liberty-figure-252202', aliases: ['libertyfigure 252202', 'liberty figure 252202'],
    name: 'LibertyFigure #252202', total: 4, base: '',
    preview: '/assets/trophies/png/5.png', puzzleImage: '/assets/trophies/png/5.png', parts: [],
  },
  {
    id: 'candy-cane-162605', aliases: ['candycane 162605', 'candy cane 162605'],
    name: 'CandyCane #162605', total: 4, base: '',
    preview: '/assets/trophies/png/6.png', puzzleImage: '/assets/trophies/png/6.png', parts: [],
  },
  {
    id: 'vice-cream-227533', aliases: ['vicecream 227533', 'vice cream 227533'],
    name: 'ViceCream #227533', total: 4, base: '',
    preview: '/assets/trophies/png/7.png', puzzleImage: '/assets/trophies/png/7.png', parts: [],
  },
  {
    id: 'vice-cream-428029', aliases: ['vicecream 428029', 'vice cream 428029'],
    name: 'ViceCream #428029', total: 4, base: '',
    preview: '/assets/trophies/png/8.png', puzzleImage: '/assets/trophies/png/8.png', parts: [],
  },
  {
    id: 'chill-flame-303522', aliases: ['chillflame 303522', 'chill flame 303522'],
    name: 'ChillFlame #303522', total: 4, base: '',
    preview: '/assets/trophies/png/9.png', puzzleImage: '/assets/trophies/png/9.png', parts: [],
  },
];

const TROPHY_CATALOG_GROUPS: { rarity: TrophyRarity; label: string; trophyIds: string[] }[] = [
  {
    rarity: 'legendary',
    label: 'Легендарные',
    trophyIds: [
      'liberty-figure-252202',
      'candy-cane-162605',
      'vice-cream-227533',
      'vice-cream-428029',
      'chill-flame-303522',
    ],
  },
  { rarity: 'rare', label: 'Редкие', trophyIds: ['bear', 'bear-redjex'] },
  { rarity: 'uncommon', label: 'Необычные', trophyIds: ['yng-explrz', 'besigned', 'stickers', 'stikidbot', 'stashvpn'] },
  { rarity: 'common', label: 'Обычные', trophyIds: ['experience', 'bomb', 'ice'] },
];

const FOUR_PART_PATHS = [
  'M1.5 35.8936H8.79883V44.4922H22.9961V22.8857H18.6963V18.8857H8.79883V30.2949H1.5V1.5H60.3896V5.79883H64.6895V64.6895H36.1895V65.9883H39.6895V70.3857H43.9883V77.5869H39.6895V81.8857H26.1895V77.5869H21.583V70.3857H26.1895V65.9883H29.1895V64.6895H1.5V35.8936Z',
  'M35.8936 64.689V57.3901H40.1924V53.0903H44.4922V47.4927H40.1924V43.1929H25.9961V47.4927H21.6963V53.0903H25.9961V57.3901H30.2949V64.689H1.5V5.79932H5.79883V1.49951H64.6895V22.4995H65.9883V18.2007H75.7012V22.4995H80V43.1929H65.9883V38.894H64.6895V64.689H35.8936Z',
  'M64.689 47.4922H57.3901V38.8936H43.1929V60.5H47.4927V64.5H57.3901V53.0908H64.689V81.8857H5.79932V77.5869H1.49951V18.6963H29.9995V17.3975H26.4995V13H22.2007V5.79883H26.4995V1.5H39.9995V5.79883H44.606V13H39.9995V17.3975H36.9995V18.6963H64.689V47.4922Z',
  'M45.6064 1.5V8.79883H41.3076V13.0986H37.0078V18.6963H41.3076V22.9961H55.5039V18.6963H59.8037V13.0986H55.5039V8.79883H51.2051V1.5H80V60.3896H75.7012V64.6895H16.8105V43.6895H15.5117V47.9883H5.79883V43.6895H1.5V22.9961H15.5117V27.2949H16.8105V1.5H45.6064Z',
] as const;

const FOUR_PART_TRANSFORMS = ['translate(57 0)', 'translate(0 0)', 'translate(0 40)', 'translate(42 57)'] as const;

function FourPartTrophyCard({ image, collected, title }: { image: string; collected: Set<number>; title: string }) {
  const clipPrefix = `trophy-${image.replace(/\D/g, '')}`;
  return (
    <svg className="trophy-four-part-card" viewBox="0 0 124 124" role="img" aria-label={title}>
      <defs>
        {FOUR_PART_PATHS.map((path, index) => (
          <clipPath id={`${clipPrefix}-part-${index + 1}`} key={`clip-${index + 1}`}>
            <path d={path} transform={FOUR_PART_TRANSFORMS[index]} />
          </clipPath>
        ))}
      </defs>
      {FOUR_PART_PATHS.map((path, index) => (
        <g key={`part-${index + 1}`}>
          <path d={path} transform={FOUR_PART_TRANSFORMS[index]} fill="#c6c6c6" />
          {collected.has(index + 1) && (
            <image href={image} x="1.5" y="1.5" width="121" height="121" preserveAspectRatio="xMidYMid slice" clipPath={`url(#${clipPrefix}-part-${index + 1})`} />
          )}
          <path d={path} transform={FOUR_PART_TRANSFORMS[index]} fill="none" stroke="#000" strokeWidth="3" strokeLinejoin="round" />
        </g>
      ))}
    </svg>
  );
}
const TWO_PART_PATHS = [
  'M7 1.5H62V47H68V43H80V47H84V61H80V65H68V61H62V122.5H7V118.5H2.5V5.5H7V1.5Z',
  'M62 1.5H117V5.5H121.5V118.5H117V122.5H62V61H68V65H80V61H84V47H80V43H68V47H62V1.5Z',
] as const;

function TwoPartTrophyCard({ image, collected, title }: { image: string; collected: Set<number>; title: string }) {
  const clipPrefix = `trophy-two-${image.replace(/\W/g, '')}`;
  return (
    <svg className="trophy-four-part-card" viewBox="0 0 124 124" role="img" aria-label={title}>
      <defs>
        {TWO_PART_PATHS.map((path, index) => (
          <clipPath id={`${clipPrefix}-part-${index + 1}`} key={`clip-${index + 1}`}><path d={path} /></clipPath>
        ))}
      </defs>
      {TWO_PART_PATHS.map((path, index) => (
        <g key={`part-${index + 1}`}>
          <path d={path} fill="#c6c6c6" />
          {collected.has(index + 1) && (
            <image href={image} x="2.5" y="1.5" width="119" height="121" preserveAspectRatio="xMidYMid slice" clipPath={`url(#${clipPrefix}-part-${index + 1})`} />
          )}
          <path d={path} fill="none" stroke="#000" strokeWidth="3" strokeLinejoin="round" />
        </g>
      ))}
    </svg>
  );
}

function FramedTrophyPreview({ image, title }: { image: string; title: string }) {
  const clipId = `trophy-preview-${image.replace(/\D/g, '')}`;
  const framePath = 'M7 1.5H117V5.5H121.5V118.5H117V122.5H7V118.5H2.5V5.5H7V1.5Z';
  return (
    <svg className="trophy-card-preview trophy-framed-preview" viewBox="0 0 124 124" role="img" aria-label={title}>
      <defs>
        <clipPath id={clipId}>
          <path d={framePath} />
        </clipPath>
      </defs>
      <image href={image} x="2.5" y="1.5" width="119" height="121" preserveAspectRatio="xMidYMid slice" clipPath={`url(#${clipId})`} />
      <path d={framePath} fill="none" stroke="#000" strokeWidth="3" strokeLinejoin="miter" />
    </svg>
  );
}

function SinglePartTrophyCard({ icon, title }: { icon: string; title: string }) {
  const framePath = 'M7 1.5H117V5.5H121.5V118.5H117V122.5H7V118.5H2.5V5.5H7V1.5Z';
  return (
    <svg className="trophy-card-preview trophy-single-part-card" viewBox="0 0 124 124" role="img" aria-label={title}>
      <path d={framePath} fill="#d7d7d7" stroke="#000" strokeWidth="3" strokeLinejoin="miter" />
      <image href={icon} x="27" y="27" width="70" height="70" preserveAspectRatio="xMidYMid meet" />
    </svg>
  );
}

function TrophyName({ name }: { name: string }) {
  const numberedName = name.match(/^(.*?)\s+(#\d+)$/);
  if (!numberedName) return <span className="trophy-name">{name}</span>;
  return <span className="trophy-name">{numberedName[1]}</span>;
}

function normalizePrizeKey(value: unknown) {
  return typeof value === 'string' ? value.trim().toLocaleLowerCase('ru-RU').replace(/[_-]+/g, ' ') : '';
}

function prizeParts(prizes: unknown[], trophy: TrophyDefinition) {
  const keys = new Set([trophy.id, trophy.name, ...trophy.aliases].map(normalizePrizeKey));
  const collected = new Set<number>();
  for (const prize of prizes) {
    if (typeof prize === 'string') {
      if (keys.has(normalizePrizeKey(prize))) collected.add(collected.size + 1);
      continue;
    }
    if (!prize || typeof prize !== 'object') continue;
    const record = prize as Record<string, unknown>;
    const key = [record.id, record.key, record.slug, record.type, record.name, record.title]
      .map(normalizePrizeKey)
      .find((candidate) => keys.has(candidate));
    if (!key) continue;
    const value = record.parts ?? record.collectedParts ?? record.collected_parts ?? record.progress ?? record.count ?? record.quantity;
    if (Array.isArray(value)) {
      value.forEach((part) => { const number = Number(part); if (Number.isInteger(number)) collected.add(number); });
    } else {
      const count = Number(value);
      for (let number = 1; number <= (Number.isFinite(count) ? count : 1); number += 1) collected.add(number);
    }
  }
  return new Set([...collected].filter((part) => part >= 1 && part <= trophy.total));
}

const GIFT_IMAGE_ASSETS = [
  '/assets/present.svg?v=2',
  '/assets/present.png?v=3',
  ...TROPHIES.flatMap((trophy) => [trophy.base, ...trophy.parts.map((part) => part.src), trophy.preview, trophy.puzzleImage, trophy.icon]
    .filter((src): src is string => Boolean(src))),
];
const GIFT_ANIMATION = '/assets/emoji_gifts.tgs?v=1';
let giftAssetsPromise: Promise<void> | null = null;

export function preloadGiftAssets() {
  if (!giftAssetsPromise) {
    const images = GIFT_IMAGE_ASSETS.map((src) => new Promise<void>((resolve) => {
        const image = new Image();
        image.onload = () => resolve();
        image.onerror = () => resolve();
        image.src = src;
      }));
    giftAssetsPromise = Promise.all([
      ...images,
      fetch(GIFT_ANIMATION).then(() => undefined).catch(() => undefined),
    ]).then(() => undefined);
  }
  return giftAssetsPromise;
}

function TrophyRewardDialog({ trophy, onClose }: { trophy: TrophyDefinition; onClose: () => void }) {
  const [reward, setReward] = useState<TrophyReward | null>(null);
  const [error, setError] = useState('');
  const [copied, setCopied] = useState(false);
  const codeRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    const initData = window.Telegram?.WebApp?.initData;
    if (!initData) {
      setError('Откройте игру через Telegram, чтобы получить награду');
      return;
    }
    const controller = new AbortController();
    const apiUrl = import.meta.env.VITE_API_URL ?? window.location.origin;
    setReward(null);
    setError('');
    const cacheKey = `${initData}\u0000${trophy.id}`;
    const cachedReward = trophyRewardCache.get(cacheKey);
    if (cachedReward) {
      setReward(cachedReward);
      return () => controller.abort();
    }
    void fetch(`${apiUrl}/api/boards/trophies/${encodeURIComponent(trophy.id)}/reward`, {
      cache: 'no-store',
      signal: controller.signal,
      headers: { 'X-Telegram-Init-Data': initData },
    }).then(async (response) => {
      if (!response.ok) {
        if (response.status === 403) throw new Error('Сначала соберите все части пазла');
        if (response.status === 410) throw new Error('Награды закончились — обратитесь к организатору');
        throw new Error('Не удалось загрузить награду. Попробуйте ещё раз');
      }
      const payload = await response.json() as Partial<TrophyReward>;
      const validCode = payload.kind === 'code' && typeof payload.value === 'string' && payload.value.length > 0;
      const validURL = payload.kind === 'url' && typeof payload.value === 'string' && payload.value.startsWith('https://');
      if (payload.trophyId !== trophy.id || (!validCode && !validURL)) throw new Error('Сервер вернул неверную награду');
      const validatedReward = payload as TrophyReward;
      trophyRewardCache.set(cacheKey, validatedReward);
      setReward(validatedReward);
    }).catch((requestError: unknown) => {
      if (!controller.signal.aborted) setError(requestError instanceof Error ? requestError.message : 'Не удалось загрузить награду');
    });
    return () => controller.abort();
  }, [trophy.id]);

  const copyCode = async () => {
    if (!reward || reward.kind !== 'code') return;
    try {
      await navigator.clipboard.writeText(reward.value);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1600);
    } catch {
      codeRef.current?.focus();
      codeRef.current?.select();
    }
  };

  return (
    <div className="trophy-reward-backdrop" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) onClose(); }}>
      <section className="trophy-reward-dialog" role="dialog" aria-modal="true" aria-labelledby="trophy-reward-title">
        <button className="trophy-reward-close" type="button" onClick={onClose} aria-label="Закрыть">×</button>
        {trophy.preview && <img className="trophy-reward-image" src={trophy.preview} alt={`Собранный пазл «${trophy.name}»`} />}
        <h2 id="trophy-reward-title">{trophy.name}</h2>
        {!reward && !error && <p className="trophy-reward-status">Загружаем награду…</p>}
        {error && <p className="trophy-reward-error">{error}</p>}
        {reward?.kind === 'code' && (
          <div className="trophy-reward-code-block">
            <label htmlFor={`reward-code-${trophy.id}`}>Ваш промокод</label>
            <div className="trophy-reward-code-row">
              <input ref={codeRef} id={`reward-code-${trophy.id}`} value={reward.value} readOnly />
              <button type="button" onClick={() => void copyCode()}>{copied ? 'Готово' : 'Копировать'}</button>
            </div>
          </div>
        )}
        {reward?.kind === 'url' && (
          <a className="trophy-reward-get" href={reward.value} target="_blank" rel="noreferrer">Получить</a>
        )}
        {reward && trophy.id === 'stashvpn' && (
          <a className="trophy-reward-get" href="https://t.me/StashNetBot" target="_blank" rel="noreferrer">Открыть бота</a>
        )}
        {reward && trophy.id === 'yng-explrz' && (
          <a className="trophy-reward-get" href="https://t.me/Bye2yngbot" target="_blank" rel="noreferrer">Открыть бота</a>
        )}
      </section>
    </div>
  );
}

function itemRewardName(reward: TrophyItemReward) {
  if (reward.item === 'experience') return `Опыт +${reward.amount}`;
  if (reward.item === 'bomb') return `Бомбы ×${reward.amount}`;
  return `Заморозки ×${reward.amount}`;
}

function ItemRewardDialog({ reward, onClose, onClaimed }: { reward: TrophyItemReward; onClose: () => void; onClaimed: (rewardId: number) => void }) {
  const [claiming, setClaiming] = useState(false);
  const [error, setError] = useState('');
  const icon = reward.item === 'bomb' ? '/assets/bomb.svg' : reward.item === 'ice' ? '/assets/ice.svg' : '/assets/exp.svg?v=2';

  const claim = async () => {
    const initData = window.Telegram?.WebApp?.initData;
    if (!initData || claiming) return;
    setClaiming(true);
    setError('');
    try {
      const apiUrl = import.meta.env.VITE_API_URL ?? window.location.origin;
      const response = await fetch(`${apiUrl}/api/boards/trophy-items/${reward.rewardId}/claim`, {
        method: 'POST',
        cache: 'no-store',
        headers: { 'X-Telegram-Init-Data': initData },
      });
      if (response.status === 404 || response.status === 409) {
        onClaimed(reward.rewardId);
        onClose();
        return;
      }
      if (!response.ok) throw new Error('Не удалось получить предмет. Попробуйте ещё раз');
      const payload = await response.json() as { claimed?: boolean };
      if (payload.claimed !== true) throw new Error('Сервер не подтвердил получение предмета');
      onClaimed(reward.rewardId);
      onClose();
      window.Telegram?.WebApp?.HapticFeedback?.notificationOccurred('success');
    } catch (claimError) {
      setError(claimError instanceof Error ? claimError.message : 'Не удалось получить предмет');
    } finally {
      setClaiming(false);
    }
  };

  return (
    <div className="trophy-reward-backdrop" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget && !claiming) onClose(); }}>
      <section className="trophy-reward-dialog" role="dialog" aria-modal="true" aria-labelledby="item-reward-title">
        <button className="trophy-reward-close" type="button" onClick={onClose} disabled={claiming} aria-label="Закрыть">×</button>
        <img className="trophy-reward-image trophy-item-reward-image" src={icon} alt="" />
        <h2 id="item-reward-title">{itemRewardName(reward)}</h2>
        <p className="trophy-reward-status">Предмет готов к получению</p>
        {error && <p className="trophy-reward-error">{error}</p>}
        <button className="trophy-reward-get" type="button" disabled={claiming} onClick={() => void claim()}>{claiming ? 'Получаем…' : 'Получить'}</button>
      </section>
    </div>
  );
}

export function GiftsScreen({ onBack, onOpenCatalog, prizes, pendingItemRewards, onItemRewardClaimed, focusRewardId, onRewardOpened }: Props) {
  const [selectedTrophy, setSelectedTrophy] = useState<TrophyDefinition | null>(null);
  const [selectedItemReward, setSelectedItemReward] = useState<TrophyItemReward | null>(null);
  const ownedTrophies = useMemo(() => TROPHIES
    .map((trophy) => ({ trophy, collected: prizeParts(prizes, trophy) }))
    .filter(({ trophy, collected }) => trophy.total > 1 && collected.size > 0), [prizes]);
  const ownedGroups = useMemo(() => TROPHY_CATALOG_GROUPS
    .map((group) => ({
      ...group,
      trophies: group.trophyIds
        .map((trophyId) => ownedTrophies.find(({ trophy }) => trophy.id === trophyId))
        .filter((entry): entry is (typeof ownedTrophies)[number] => Boolean(entry)),
    }))
    .filter(({ trophies }) => trophies.length > 0), [ownedTrophies]);

  useEffect(() => {
    if (!focusRewardId) return;
    const completed = ownedTrophies.find(({ trophy, collected }) => trophy.id === focusRewardId
      && trophy.total > 1 && collected.size >= trophy.total);
    if (!completed) return;
    setSelectedTrophy(completed.trophy);
    onRewardOpened?.();
  }, [focusRewardId, onRewardOpened, ownedTrophies]);

  return (
    <div className="gifts-screen">
      <img className="gifts-logo" src="/assets/present.png?v=3" alt="Трофеи" />
      {ownedTrophies.length === 0 && pendingItemRewards.length === 0 && (
        <section className="gifts-empty" aria-label="Инвентарь трофеев пуст">
          <TgsPlayer className="gifts-empty-icon" src={GIFT_ANIMATION} loop={false} />
          <p className="gifts-empty-title">В вашем инвентаре<br />еще нет предметов</p>
          <p className="gifts-empty-hint">Вы можете получить трофеи,<br />просто играя в PixelBattle</p>
        </section>
      )}
      {ownedTrophies.length > 0 && (
        <section className="gifts-inventory" aria-label="Полученные трофеи">
          {pendingItemRewards.length > 0 && (
            <section className="trophy-rarity-section trophy-rarity-common">
              <header className="trophy-rarity-heading">
                <span className="trophy-rarity-mark" aria-hidden="true" />
                <h2>Обычные</h2>
                <span className="trophy-rarity-line" aria-hidden="true" />
              </header>
              <div className="trophy-rarity-grid">
                {pendingItemRewards.map((reward) => {
                  const icon = reward.item === 'bomb' ? '/assets/bomb.svg' : reward.item === 'ice' ? '/assets/ice.svg' : '/assets/exp.svg?v=2';
                  return (
                    <button className={`trophy-item trophy-reward-open trophy-item-${reward.item}`} type="button" onClick={() => setSelectedItemReward(reward)} key={reward.rewardId} aria-label={`Получить «${itemRewardName(reward)}»`}>
                      <div className="trophy-card"><SinglePartTrophyCard icon={icon} title={itemRewardName(reward)} /></div>
                      <div className="trophy-caption"><TrophyName name={itemRewardName(reward)} /></div>
                    </button>
                  );
                })}
              </div>
            </section>
          )}
          {ownedGroups.map((group) => (
            <section className={`trophy-rarity-section trophy-rarity-${group.rarity}`} key={group.rarity}>
              <header className="trophy-rarity-heading">
                <span className="trophy-rarity-mark" aria-hidden="true" />
                <h2>{group.label}</h2>
                <span className="trophy-rarity-line" aria-hidden="true" />
              </header>
              <div className="trophy-rarity-grid">
                {group.trophies.map(({ trophy, collected }) => {
                  const completed = trophy.total > 1 && collected.size >= trophy.total;
                  const content = <>
                    <div className="trophy-card">
                      {trophy.icon && collected.size >= trophy.total ? (
                        <SinglePartTrophyCard icon={trophy.icon} title={trophy.name} />
                      ) : trophy.preview && collected.size >= trophy.total ? (
                        trophy.puzzleImage ? (
                          <FramedTrophyPreview image={trophy.preview} title={trophy.name} />
                        ) : (
                          <img className="trophy-card-preview" src={trophy.preview} alt={trophy.name} />
                        )
                      ) : trophy.puzzleImage && trophy.total === 2 ? (
                        <TwoPartTrophyCard image={trophy.puzzleImage} collected={collected} title={trophy.name} />
                      ) : trophy.puzzleImage ? (
                        <FourPartTrophyCard image={trophy.puzzleImage} collected={collected} title={trophy.name} />
                      ) : (
                        <>
                          <img className="trophy-card-base" src={trophy.base} alt="" />
                          {trophy.parts.map((part) => collected.has(part.number) && (
                            <img className={`trophy-card-part ${part.className}`} src={part.src} alt="" key={part.src} />
                          ))}
                        </>
                      )}
                    </div>
                    <div className="trophy-caption">
                      <TrophyName name={trophy.name} />
                      {trophy.total > 1 && <span className="trophy-progress">{collected.size}/{trophy.total} частей</span>}
                    </div>
                  </>;
                  return completed ? (
                    <button className={`trophy-item trophy-reward-open trophy-item-${trophy.id}`} type="button" onClick={() => setSelectedTrophy(trophy)} key={trophy.id} aria-label={`Открыть награду «${trophy.name}»`}>
                      {content}
                    </button>
                  ) : (
                    <article className={`trophy-item trophy-item-${trophy.id}`} key={trophy.id}>{content}</article>
                  );
                })}
              </div>
            </section>
          ))}
          <button className="gifts-catalog-link gifts-catalog-link-flow" onClick={onOpenCatalog}>Полный список доступных наград</button>
        </section>
      )}
      {ownedTrophies.length === 0 && pendingItemRewards.length === 0 && (
        <button className="gifts-catalog-link" onClick={onOpenCatalog}>Полный список доступных наград</button>
      )}
      {ownedTrophies.length === 0 && pendingItemRewards.length > 0 && (
        <section className="gifts-inventory" aria-label="Полученные предметы">
          <section className="trophy-rarity-section trophy-rarity-common">
            <header className="trophy-rarity-heading"><span className="trophy-rarity-mark" aria-hidden="true" /><h2>Обычные</h2><span className="trophy-rarity-line" aria-hidden="true" /></header>
            <div className="trophy-rarity-grid">
              {pendingItemRewards.map((reward) => {
                const icon = reward.item === 'bomb' ? '/assets/bomb.svg' : reward.item === 'ice' ? '/assets/ice.svg' : '/assets/exp.svg?v=2';
                return <button className={`trophy-item trophy-reward-open trophy-item-${reward.item}`} type="button" onClick={() => setSelectedItemReward(reward)} key={reward.rewardId}><div className="trophy-card"><SinglePartTrophyCard icon={icon} title={itemRewardName(reward)} /></div><div className="trophy-caption"><TrophyName name={itemRewardName(reward)} /></div></button>;
              })}
            </div>
          </section>
          <button className="gifts-catalog-link gifts-catalog-link-flow" onClick={onOpenCatalog}>Полный список доступных наград</button>
        </section>
      )}
      <button className="stats-back" onClick={onBack}>Назад</button>
      {selectedTrophy && <TrophyRewardDialog trophy={selectedTrophy} onClose={() => setSelectedTrophy(null)} />}
      {selectedItemReward && <ItemRewardDialog reward={selectedItemReward} onClose={() => setSelectedItemReward(null)} onClaimed={onItemRewardClaimed} />}
    </div>
  );
}

export function GiftsCatalogScreen({ onBack, prizes, soldOutTrophies }: CatalogProps) {
  const soldOut = new Set(soldOutTrophies);
  const availableGroups = TROPHY_CATALOG_GROUPS
    .map((group) => ({ ...group, trophyIds: group.trophyIds.filter((id) => !soldOut.has(id)) }))
    .filter((group) => group.trophyIds.length > 0);
  return (
    <div className="gifts-catalog-screen">
      <img className="gifts-logo" src="/assets/present.png?v=3" alt="Доступные награды" />
      <section className="trophy-catalog" aria-label="Полный список доступных наград">
        {availableGroups.map((group) => (
          <section className={`trophy-rarity-section trophy-rarity-${group.rarity}`} key={group.rarity}>
            <header className="trophy-rarity-heading">
              <span className="trophy-rarity-mark" aria-hidden="true" />
              <h2>{group.label}</h2>
              <span className="trophy-rarity-line" aria-hidden="true" />
            </header>
            <div className="trophy-rarity-grid">
              {group.trophyIds
                .map((trophyId) => TROPHIES.find((item) => item.id === trophyId))
                .filter((trophy): trophy is TrophyDefinition => Boolean(trophy))
                .map((trophy) => {
                  const collected = prizeParts(prizes, trophy);
                  return (
                    <article className={`trophy-item trophy-item-${trophy.id}`} key={trophy.id}>
                      <div className="trophy-card">
                        {trophy.icon ? (
                          <SinglePartTrophyCard icon={trophy.icon} title={trophy.name} />
                        ) : trophy.preview ? (
                          trophy.puzzleImage ? (
                            <FramedTrophyPreview image={trophy.preview} title={trophy.name} />
                          ) : (
                            <img className="trophy-card-preview" src={trophy.preview} alt={trophy.name} />
                          )
                        ) : (
                          <>
                            <img className="trophy-card-base" src={trophy.base} alt="" />
                            {trophy.parts.map((part) => collected.has(part.number) && (
                              <img className={`trophy-card-part ${part.className}`} src={part.src} alt="" key={part.src} />
                            ))}
                          </>
                        )}
                      </div>
                      <div className="trophy-caption">
                        <TrophyName name={trophy.name} />
                      </div>
                    </article>
                  );
                })}
            </div>
          </section>
        ))}
      </section>
      <button className="stats-back" onClick={onBack}>Назад</button>
    </div>
  );
}

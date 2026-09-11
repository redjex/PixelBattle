import { TgsPlayer } from './TgsPlayer';

type Props = { onBack: () => void; onOpenCatalog: () => void; prizes: unknown[] };
type CatalogProps = { onBack: () => void; prizes: unknown[] };

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
  parts: TrophyPart[];
};

type TrophyRarity = 'legendary' | 'rare' | 'common';

const TROPHIES: TrophyDefinition[] = [
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
    id: 'bear', aliases: ['bear', 'мишка'], name: 'Мишка', total: 2,
    base: '/assets/trophies/two-part-base.svg',
    preview: '/assets/trophies/bear-main.svg?v=4',
    parts: [
      { src: '/assets/trophies/bear-1v2.svg?v=3', className: 'trophy-part-half-right', number: 1 },
      { src: '/assets/trophies/bear-2v2.svg?v=3', className: 'trophy-part-half-left', number: 2 },
    ],
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
  { rarity: 'rare', label: 'Редкие', trophyIds: ['bear'] },
  { rarity: 'common', label: 'Обычные', trophyIds: ['yng-explrz', 'besigned', 'stickers'] },
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
  '/assets/present.png?v=2',
  ...TROPHIES.flatMap((trophy) => [trophy.base, ...trophy.parts.map((part) => part.src), trophy.preview, trophy.puzzleImage]
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

export function GiftsScreen({ onBack, onOpenCatalog, prizes }: Props) {
  const ownedTrophies = TROPHIES
    .map((trophy) => ({ trophy, collected: prizeParts(prizes, trophy) }))
    .filter(({ collected }) => collected.size > 0);
  const ownedGroups = TROPHY_CATALOG_GROUPS
    .map((group) => ({
      ...group,
      trophies: group.trophyIds
        .map((trophyId) => ownedTrophies.find(({ trophy }) => trophy.id === trophyId))
        .filter((entry): entry is (typeof ownedTrophies)[number] => Boolean(entry)),
    }))
    .filter(({ trophies }) => trophies.length > 0);
  return (
    <div className="gifts-screen">
      <img className="gifts-logo" src="/assets/present.png?v=2" alt="Трофеи" />
      {prizes.length === 0 && (
        <section className="gifts-empty" aria-label="Инвентарь трофеев пуст">
          <TgsPlayer className="gifts-empty-icon" src={GIFT_ANIMATION} loop={false} />
          <p className="gifts-empty-title">В вашем инвентаре<br />еще нет предметов</p>
          <p className="gifts-empty-hint">Вы можете получить трофеи,<br />просто играя в PixelBattle</p>
        </section>
      )}
      {ownedTrophies.length > 0 && (
        <section className="gifts-inventory" aria-label="Полученные трофеи">
          {ownedGroups.map((group) => (
            <section className={`trophy-rarity-section trophy-rarity-${group.rarity}`} key={group.rarity}>
              <header className="trophy-rarity-heading">
                <span className="trophy-rarity-mark" aria-hidden="true" />
                <h2>{group.label}</h2>
                <span className="trophy-rarity-line" aria-hidden="true" />
              </header>
              <div className="trophy-rarity-grid">
                {group.trophies.map(({ trophy, collected }) => (
                  <article className={`trophy-item trophy-item-${trophy.id}`} key={trophy.id}>
                    <div className="trophy-card">
                      {trophy.preview && collected.size >= trophy.total ? (
                        trophy.puzzleImage ? (
                          <FramedTrophyPreview image={trophy.preview} title={trophy.name} />
                        ) : (
                          <img className="trophy-card-preview" src={trophy.preview} alt={trophy.name} />
                        )
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
                      <span className="trophy-progress">{collected.size}/{trophy.total} частей</span>
                    </div>
                  </article>
                ))}
              </div>
            </section>
          ))}
          <button className="gifts-catalog-link gifts-catalog-link-flow" onClick={onOpenCatalog}>Полный список доступных наград</button>
        </section>
      )}
      {ownedTrophies.length === 0 && (
        <button className="gifts-catalog-link" onClick={onOpenCatalog}>Полный список доступных наград</button>
      )}
      <button className="stats-back" onClick={onBack}>Назад</button>
    </div>
  );
}

export function GiftsCatalogScreen({ onBack, prizes }: CatalogProps) {
  return (
    <div className="gifts-catalog-screen">
      <img className="gifts-logo" src="/assets/present.png?v=2" alt="Доступные награды" />
      <section className="trophy-catalog" aria-label="Полный список доступных наград">
        {TROPHY_CATALOG_GROUPS.map((group) => (
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
                        {trophy.preview ? (
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

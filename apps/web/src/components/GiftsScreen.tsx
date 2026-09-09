type Props = { onBack: () => void; onOpenCatalog: () => void; prizes: unknown[] };
type CatalogProps = { onBack: () => void };

const GIFT_ASSETS = ['/assets/profile.svg?v=1', '/assets/present.svg?v=1', '/assets/present.png?v=2', '/assets/emoji_gifts.png?v=1'];
let giftAssetsPromise: Promise<void> | null = null;

export function preloadGiftAssets() {
  if (!giftAssetsPromise) {
    giftAssetsPromise = Promise.all(GIFT_ASSETS.map((src) => new Promise<void>((resolve) => {
      const image = new Image();
      image.onload = () => resolve();
      image.onerror = () => resolve();
      image.src = src;
    }))).then(() => undefined);
  }
  return giftAssetsPromise;
}

export function GiftsScreen({ onBack, onOpenCatalog, prizes }: Props) {
  return (
    <div className="gifts-screen">
      <img className="gifts-logo" src="/assets/present.png?v=2" alt="Трофеи" />
      {prizes.length === 0 && (
        <section className="gifts-empty" aria-label="Инвентарь трофеев пуст">
          <img className="gifts-empty-icon" src="/assets/emoji_gifts.png?v=1" alt="" />
          <p className="gifts-empty-title">В вашем инвентаре<br />еще нет предметов</p>
          <p className="gifts-empty-hint">Вы можете получить трофеи,<br />просто играя в PixelBattle</p>
        </section>
      )}
      <button className="gifts-catalog-link" onClick={onOpenCatalog}>Полный список доступных наград</button>
      <button className="stats-back" onClick={onBack}>Назад</button>
    </div>
  );
}

export function GiftsCatalogScreen({ onBack }: CatalogProps) {
  return (
    <div className="gifts-catalog-screen">
      <img className="gifts-logo" src="/assets/present.png?v=2" alt="Доступные награды" />
      <button className="stats-back" onClick={onBack}>Назад</button>
    </div>
  );
}

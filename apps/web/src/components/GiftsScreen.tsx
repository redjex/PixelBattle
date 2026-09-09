type Props = { onBack: () => void };

const GIFT_ASSETS = ['/assets/gifts.png?v=2', '/assets/present.png?v=2'];
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

export function GiftsScreen({ onBack }: Props) {
  return (
    <div className="gifts-screen">
      <img className="gifts-logo" src="/assets/present.png?v=2" alt="Трофеи" />
      <button className="stats-back" onClick={onBack}>Назад</button>
    </div>
  );
}

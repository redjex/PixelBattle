import { ParallaxBackground } from './ParallaxBackground';
import { SeasonStatus } from './SeasonStatus';

type Props = { onOpenMap: () => void; onOpenStats: () => void; onOpenGifts: () => void; maintenance: boolean; online: number | null };

export function MainMenu({ onOpenMap, onOpenStats, onOpenGifts, maintenance, online }: Props) {
  return (
    <div className="main-menu" data-node-id="1878:25595">
      <ParallaxBackground />
      <SeasonStatus online={online} showOnline />
      <img className="menu-logo" src="/assets/pixel_logo.png" alt="Pixel Battle" />
      <div className="menu-actions">
        <div className="menu-shortcuts">
          <button className="menu-shortcut" onClick={onOpenStats} disabled={maintenance} aria-label="Открыть профиль">
            <img src="/assets/profile.svg?v=1" alt="" />
          </button>
          <button className="menu-shortcut" onClick={onOpenGifts} disabled={maintenance} aria-label="Открыть трофеи">
            <img src="/assets/present.svg?v=1" alt="" />
          </button>
        </div>
        <button className="menu-button menu-button-primary" onClick={onOpenMap} disabled={maintenance}>{maintenance ? 'Тех. обслуживание' : 'Открыть карту'}</button>
      </div>
    </div>
  );
}

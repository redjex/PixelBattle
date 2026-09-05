import { ParallaxBackground } from './ParallaxBackground';

type Props = { onOpenMap: () => void; onOpenStats: () => void };

export function MainMenu({ onOpenMap, onOpenStats }: Props) {
  return (
    <div className="main-menu" data-node-id="1878:25595">
      <ParallaxBackground />
      <img className="menu-logo" src="/assets/pixel_logo.png" alt="Pixel Battle" />
      <div className="menu-actions">
        <button className="menu-button menu-button-primary" onClick={onOpenMap}>Открыть карту</button>
        <button className="menu-button menu-button-secondary" onClick={onOpenStats}><span>Статистика</span></button>
      </div>
    </div>
  );
}

import { ParallaxBackground } from './ParallaxBackground';

type Props = { active: boolean; onOpenMap: () => void; onOpenStats: () => void; onOpenGifts: () => void; maintenance: boolean };

export function MainMenu({ active, onOpenMap, onOpenStats, onOpenGifts, maintenance }: Props) {
  return (
    <div className={`main-menu${active ? '' : ' app-view-hidden'}`} data-node-id="1878:25595" aria-hidden={!active}>
      <ParallaxBackground active={active} />
      <img className="menu-logo" src="/assets/pixel_logo.png" alt="Pixel Battle" />
      <div className="menu-actions">
        <div className="menu-shortcuts">
          <button className="menu-shortcut menu-shortcut-profile" onClick={onOpenStats} disabled={maintenance} aria-label="Открыть профиль">
            <span>Профиль</span>
          </button>
          <button className="menu-shortcut menu-shortcut-present" onClick={onOpenGifts} disabled={maintenance} aria-label="Открыть трофеи">
            <svg className="menu-shortcut-crown" viewBox="102 0 51 24" aria-hidden="true">
              <path d="M109.407 21.019c.703 0 1.187-.707.932-1.363l-3.906-10.032c-.33-.851.557-1.664 1.375-1.26l11.115 5.484c.471.232 1.041.061 1.306-.392l6.408-10.961c.386-.66 1.34-.66 1.726 0l6.408 10.961c.265.453.835.624 1.306.392l11.115-5.484c.818-.404 1.705.409 1.375 1.26l-3.906 10.032c-.255.656.229 1.363.932 1.363Z" fill="#fff" />
              <path d="M109.407 21.019c.703 0 1.187-.707.932-1.363l-3.906-10.032c-.33-.851.557-1.664 1.375-1.26l11.115 5.484c.471.232 1.041.061 1.306-.392l6.408-10.961c.386-.66 1.34-.66 1.726 0l6.408 10.961c.265.453.835.624 1.306.392l11.115-5.484c.818-.404 1.705.409 1.375 1.26l-3.906 10.032c-.255.656.229 1.363.932 1.363" fill="none" stroke="#000" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
            </svg>
            <span>Трофеи</span>
          </button>
        </div>
        <button className="menu-button menu-button-primary" onClick={onOpenMap} disabled={maintenance}>{maintenance ? 'Тех. обслуживание' : 'Открыть карту'}</button>
      </div>
    </div>
  );
}

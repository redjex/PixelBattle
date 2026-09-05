import { TgsPlayer } from './TgsPlayer';
import { ParallaxBackground } from './ParallaxBackground';

export function LoadingScreen({ message = 'Загрузка...' }: { message?: string }) {
  return (
    <div className="loading-screen" aria-label="Загрузка Pixel Battle">
      <ParallaxBackground />
      <img className="loading-logo" src="/assets/pixel_logo.png" alt="Pixel Battle" />
      <div className="loading-content">
        <TgsPlayer className="loading-animation" src="/assets/loader.json" />
        <span>{message}</span>
      </div>
    </div>
  );
}

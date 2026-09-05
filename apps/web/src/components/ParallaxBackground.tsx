import { useEffect, useRef, useState, type CSSProperties } from 'react';
import { getTelegramWebApp } from '../telegram';

// Layers are ordered from the farthest to the nearest. Their movement grows
// exponentially towards the foreground instead of changing by equal steps.
const layerSources = [
  '/assets/fone/landscape.png',
  '/assets/fone/behind-mountains.png',
  '/assets/fone/after-behind-mountains.png',
  '/assets/fone/clouds.png',
  '/assets/fone/right-mountain.png',
  '/assets/fone/plan-2.png',
  '/assets/fone/plan-2-5.png',
  '/assets/fone/plan-1.png',
  '/assets/fone/moon-3.png',
] as const;

const layers = layerSources.map((src, index) => {
  const foregroundPosition = index / (layerSources.length - 1);
  return {
    src,
    depth: 0.015 + 0.985 * Math.pow(foregroundPosition, 2.4),
  };
});

const startupImageSources = [
  ...layers.map(({ src }) => src),
  '/assets/raiting.png',
  '/assets/bomb.svg',
  '/assets/ice.svg',
];

type DeviceOrientationPermissionEvent = typeof DeviceOrientationEvent & {
  requestPermission?: () => Promise<'granted' | 'denied'>;
};

let sensorPermissionGranted = false;
let parallaxAssetsReady = false;
let parallaxPreloadPromise: Promise<void> | null = null;

export function preloadParallaxBackground() {
  if (parallaxAssetsReady) return Promise.resolve();
  if (parallaxPreloadPromise) return parallaxPreloadPromise;

  parallaxPreloadPromise = Promise.all(startupImageSources.map((src) => new Promise<void>((resolve, reject) => {
    const image = new Image();
    image.onload = () => {
      if (typeof image.decode === 'function') {
        void image.decode().catch(() => undefined).then(() => resolve());
      } else {
        resolve();
      }
    };
    image.onerror = () => reject(new Error(`Unable to preload background layer: ${src}`));
    image.src = src;
  }))).then(() => {
    parallaxAssetsReady = true;
  }).catch((error) => {
    parallaxPreloadPromise = null;
    throw error;
  });

  return parallaxPreloadPromise;
}

export function ParallaxBackground() {
  const rootRef = useRef<HTMLDivElement>(null);
  const [assetsReady, setAssetsReady] = useState(parallaxAssetsReady);

  useEffect(() => {
    let active = true;
    void preloadParallaxBackground().then(() => {
      if (active) setAssetsReady(true);
    }).catch(() => undefined);
    return () => { active = false; };
  }, []);

  useEffect(() => {
    const root = rootRef.current;
    if (!root || window.matchMedia('(prefers-reduced-motion: reduce)').matches) return;

    let animationFrame = 0;
    let browserListening = false;
    let telegramListening = false;
    let disposed = false;
    let originBeta: number | null = null;
    let originGamma: number | null = null;
    let targetX = 0;
    let targetY = 0;
    let currentX = 0;
    let currentY = 0;

    const render = () => {
      currentX += (targetX - currentX) * 0.09;
      currentY += (targetY - currentY) * 0.09;
      root.style.setProperty('--parallax-x', currentX.toFixed(3));
      root.style.setProperty('--parallax-y', currentY.toFixed(3));

      if (Math.abs(targetX - currentX) > 0.001 || Math.abs(targetY - currentY) > 0.001) {
        animationFrame = window.requestAnimationFrame(render);
      } else {
        animationFrame = 0;
      }
    };

    const scheduleRender = () => {
      if (!animationFrame) animationFrame = window.requestAnimationFrame(render);
    };

    const updateOrientation = (beta: number, gamma: number, valuesAreRadians = false) => {
      const normalizedBeta = valuesAreRadians ? beta * 180 / Math.PI : beta;
      const normalizedGamma = valuesAreRadians ? gamma * 180 / Math.PI : gamma;
      if (originBeta === null || originGamma === null) {
        originBeta = normalizedBeta;
        originGamma = normalizedGamma;
      }

      targetX = Math.max(-1, Math.min(1, (normalizedGamma - originGamma) / 24));
      targetY = Math.max(-1, Math.min(1, (normalizedBeta - originBeta) / 24));
      scheduleRender();
    };

    const handleBrowserOrientation = (event: DeviceOrientationEvent) => {
      if (event.beta === null || event.gamma === null) return;
      updateOrientation(event.beta, event.gamma);
    };

    const startBrowserListening = () => {
      if (browserListening || disposed) return;
      window.addEventListener('deviceorientation', handleBrowserOrientation, { passive: true });
      browserListening = true;
    };

    const requestBrowserSensor = async () => {
      if (typeof DeviceOrientationEvent === 'undefined') return;
      const orientation = DeviceOrientationEvent as DeviceOrientationPermissionEvent;
      if (typeof orientation.requestPermission === 'function') {
        try {
          if (await orientation.requestPermission() === 'granted') {
            sensorPermissionGranted = true;
            startBrowserListening();
          }
        } catch {
          // iOS rejects requests that are not initiated by a user gesture.
        }
      } else {
        startBrowserListening();
      }
    };

    const handleFirstGesture = () => {
      void requestBrowserSensor();
      document.removeEventListener('click', handleFirstGesture, true);
    };

    const prepareBrowserFallback = () => {
      const orientation = typeof DeviceOrientationEvent === 'undefined'
        ? undefined
        : DeviceOrientationEvent as DeviceOrientationPermissionEvent;
      if (sensorPermissionGranted) {
        startBrowserListening();
      } else if (typeof orientation?.requestPermission === 'function') {
        document.addEventListener('click', handleFirstGesture, { passive: true, capture: true });
      } else if (orientation) {
        startBrowserListening();
      }
    };

    const telegram = getTelegramWebApp();
    const telegramOrientation = telegram?.DeviceOrientation;
    const handleTelegramOrientation = () => {
      const beta = telegramOrientation?.beta;
      const gamma = telegramOrientation?.gamma;
      if (typeof beta === 'number' && typeof gamma === 'number') {
        updateOrientation(beta, gamma, true);
      }
    };

    if (telegramOrientation?.start) {
      telegram?.onEvent?.('deviceOrientationChanged', handleTelegramOrientation);
      telegramListening = true;
      try {
        telegramOrientation.start({ refresh_rate: 30, need_absolute: false }, (started) => {
          if (disposed) return;
          if (started) {
            handleTelegramOrientation();
          } else {
            telegram?.offEvent?.('deviceOrientationChanged', handleTelegramOrientation);
            telegramListening = false;
            prepareBrowserFallback();
          }
        });
      } catch {
        telegram?.offEvent?.('deviceOrientationChanged', handleTelegramOrientation);
        telegramListening = false;
        prepareBrowserFallback();
      }
    } else {
      prepareBrowserFallback();
    }

    return () => {
      disposed = true;
      document.removeEventListener('click', handleFirstGesture, true);
      if (browserListening) window.removeEventListener('deviceorientation', handleBrowserOrientation);
      if (telegramListening) {
        telegram?.offEvent?.('deviceOrientationChanged', handleTelegramOrientation);
        telegramOrientation?.stop();
      }
      if (animationFrame) window.cancelAnimationFrame(animationFrame);
    };
  }, []);

  return (
    <div ref={rootRef} className={`parallax-background${assetsReady ? ' ready' : ''}`} aria-hidden="true">
      {layers.map((layer) => (
        <img
          className="parallax-layer"
          key={layer.src}
          src={layer.src}
          alt=""
          draggable={false}
          style={{ '--layer-depth': layer.depth } as CSSProperties}
        />
      ))}
    </div>
  );
}

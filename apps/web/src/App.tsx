import { useEffect, useState } from 'react';
import { BattleScreen } from './components/BattleScreen';
import { LoadingScreen } from './components/LoadingScreen';
import { MainMenu } from './components/MainMenu';
import { StatisticsScreen } from './components/StatisticsScreen';
import { AgreementScreen } from './components/AgreementScreen';
import { authenticateTelegram, getTelegramWebApp } from './telegram';
import { preloadBoardSnapshot } from './boardSnapshot';
import { preloadStatistics } from './statisticsCache';
import { QuestNotifications } from './components/QuestNotifications';
import { preloadParallaxBackground } from './components/ParallaxBackground';
import { preloadRatingRewards, RatingScreen } from './components/RatingScreen';

export function App() {
  const [loading, setLoading] = useState(true);
  const [authState, setAuthState] = useState<'checking' | 'denied' | 'invalid' | 'authorized'>('checking');
  const [screen, setScreen] = useState<'menu' | 'map' | 'stats' | 'rating' | 'agreement'>('menu');

  useEffect(() => {
    void preloadParallaxBackground().catch(() => undefined);
  }, []);

  useEffect(() => {
    const blockZoom = (event: WheelEvent) => {
      if (!event.ctrlKey && !event.metaKey) return;
      event.preventDefault();
      event.stopPropagation();
    };
    const blockGesture = (event: Event) => {
      event.preventDefault();
      event.stopPropagation();
    };
    window.addEventListener('wheel', blockZoom, { passive: false, capture: true });
    document.addEventListener('gesturestart', blockGesture, { passive: false, capture: true });
    document.addEventListener('gesturechange', blockGesture, { passive: false, capture: true });
    document.addEventListener('gestureend', blockGesture, { passive: false, capture: true });
    return () => {
      window.removeEventListener('wheel', blockZoom, { capture: true });
      document.removeEventListener('gesturestart', blockGesture, { capture: true });
      document.removeEventListener('gesturechange', blockGesture, { capture: true });
      document.removeEventListener('gestureend', blockGesture, { capture: true });
    };
  }, []);

  useEffect(() => {
    const telegram = getTelegramWebApp();
    if (!telegram) return;
    const syncSafeArea = () => {
      const root = document.documentElement;
      const safe = telegram.safeAreaInset;
      const content = telegram.contentSafeAreaInset;
      root.style.setProperty('--tg-safe-area-inset-top', `${safe?.top ?? 0}px`);
      root.style.setProperty('--tg-safe-area-inset-bottom', `${safe?.bottom ?? 0}px`);
      root.style.setProperty('--tg-safe-area-inset-left', `${safe?.left ?? 0}px`);
      root.style.setProperty('--tg-safe-area-inset-right', `${safe?.right ?? 0}px`);
      root.style.setProperty('--tg-content-safe-area-inset-top', `${content?.top ?? 0}px`);
      root.style.setProperty('--tg-content-safe-area-inset-bottom', `${content?.bottom ?? 0}px`);
      root.style.setProperty('--tg-content-safe-area-inset-left', `${content?.left ?? 0}px`);
      root.style.setProperty('--tg-content-safe-area-inset-right', `${content?.right ?? 0}px`);
    };
    syncSafeArea();
    telegram.onEvent?.('safeAreaChanged', syncSafeArea);
    telegram.onEvent?.('contentSafeAreaChanged', syncSafeArea);
    telegram.onEvent?.('fullscreenChanged', syncSafeArea);
    return () => {
      telegram.offEvent?.('safeAreaChanged', syncSafeArea);
      telegram.offEvent?.('contentSafeAreaChanged', syncSafeArea);
      telegram.offEvent?.('fullscreenChanged', syncSafeArea);
    };
  }, []);

  useEffect(() => {
    const backButton = getTelegramWebApp()?.BackButton;
    if (!backButton) return;
    if (loading || screen === 'menu') {
      backButton.hide();
      return;
    }
    const handleBack = () => setScreen(screen === 'agreement' || screen === 'rating' ? 'stats' : 'menu');
    backButton.onClick(handleBack);
    backButton.show();
    return () => {
      backButton.offClick(handleBack);
      backButton.hide();
    };
  }, [loading, screen]);

  useEffect(() => {
    const telegram = getTelegramWebApp();
    if (!telegram?.initData) { setAuthState('denied'); return; }
    telegram.ready();
    // This game uses the whole Mini App surface for drawing and panning.
    // Prevent Telegram's vertical swipe gesture from minimizing/closing it.
    // The method is optional because older Telegram clients do not expose it.
    telegram.disableVerticalSwipes?.();
    telegram.expand();
    authenticateTelegram(telegram.initData).then((valid) => {
      if (valid) {
        void preloadBoardSnapshot(telegram.initData).catch(() => undefined);
        void preloadStatistics(telegram.initData).catch(() => undefined);
        void preloadRatingRewards(telegram.initData).catch(() => undefined);
        const avatarUrl = telegram.initDataUnsafe?.user?.photo_url;
        if (avatarUrl) {
          const avatar = new Image();
          avatar.src = avatarUrl;
        }
      }
      setAuthState(valid ? 'authorized' : 'invalid');
    }).catch(() => setAuthState('invalid'));
  }, []);

  useEffect(() => {
    if (authState !== 'authorized') return;
    const telegram = getTelegramWebApp();
    telegram?.disableVerticalSwipes?.();
    telegram?.expand();
    const desktopPlatforms = new Set(['tdesktop', 'macos', 'web', 'weba', 'webk']);
    const isDesktop = desktopPlatforms.has(telegram?.platform ?? '')
      || (window.matchMedia('(pointer: fine)').matches && window.innerWidth >= 700);
    if (isDesktop) {
      if (telegram?.isFullscreen) telegram.exitFullscreen?.();
    } else {
      telegram?.requestFullscreen?.();
      if (!telegram?.requestFullscreen && document.documentElement.requestFullscreen) {
        void document.documentElement.requestFullscreen().catch(() => undefined);
      }
    }
    const duration = Number(import.meta.env.VITE_SPLASH_DURATION_MS ?? 2200);
    let active = true;
    let timer = 0;
    const minimumSplash = new Promise<void>((resolve) => {
      timer = window.setTimeout(resolve, duration);
    });
    const rewardsReady = telegram?.initData
      ? preloadRatingRewards(telegram.initData).catch(() => undefined)
      : Promise.resolve();
    void Promise.all([minimumSplash, preloadParallaxBackground(), rewardsReady]).then(() => {
      if (active) setLoading(false);
    }).catch(() => {
      // Keep the loading screen visible if a required layer could not be loaded.
    });
    return () => {
      active = false;
      window.clearTimeout(timer);
    };
  }, [authState]);

  if (authState !== 'authorized') return <main className="app-shell"><section className="phone-frame"><LoadingScreen message={authState === 'invalid' ? 'Ошибка проверки Telegram' : 'Откройте через Telegram'} /></section></main>;
  return <main className="app-shell"><section className="phone-frame" aria-label="Pixel Battle">
    {loading ? <LoadingScreen /> : screen === 'menu' ? <MainMenu onOpenMap={() => setScreen('map')} onOpenStats={() => setScreen('stats')} /> : screen === 'stats' ? <StatisticsScreen onBack={() => setScreen('menu')} onOpenRating={() => setScreen('rating')} onOpenAgreement={() => setScreen('agreement')} /> : screen === 'rating' ? <RatingScreen onBack={() => setScreen('stats')} /> : screen === 'agreement' ? <AgreementScreen onBack={() => setScreen('stats')} /> : <BattleScreen />}
    {!loading && <QuestNotifications />}
  </section></main>;
}

import { useEffect, useState } from 'react';
import { BattleScreen } from './components/BattleScreen';
import { LoadingScreen } from './components/LoadingScreen';
import { MainMenu } from './components/MainMenu';
import { StatisticsScreen } from './components/StatisticsScreen';
import { SettingsScreen } from './components/SettingsScreen';
import { AgreementScreen } from './components/AgreementScreen';
import { authenticateTelegram, fetchAppAccess, getTelegramWebApp, TelegramAuthenticationError, type AppAccess } from './telegram';
import { preloadBoardSnapshot } from './boardSnapshot';
import { preloadStatistics } from './statisticsCache';
import { QuestNotifications } from './components/QuestNotifications';
import { preloadParallaxBackground } from './components/ParallaxBackground';
import { preloadRatingRewards, RatingScreen } from './components/RatingScreen';
import { GiftsCatalogScreen, GiftsScreen, preloadGiftAssets } from './components/GiftsScreen';
import { CaptchaOverlay } from './components/CaptchaOverlay';

export function App() {
  const [loading, setLoading] = useState(true);
  const [authState, setAuthState] = useState<'checking' | 'denied' | 'invalid' | 'authorized'>('checking');
  const [appAccess, setAppAccess] = useState<AppAccess | null>(null);
  const [screen, setScreen] = useState<'menu' | 'map' | 'stats' | 'rating' | 'agreement' | 'settings' | 'gifts' | 'gifts-catalog'>('menu');
  const maintenanceMode = appAccess?.accessAllowed === false;

  useEffect(() => {
    void preloadParallaxBackground().catch(() => undefined);
    void preloadGiftAssets().catch(() => undefined);
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
    const handleBack = () => setScreen(screen === 'agreement' || screen === 'rating' || screen === 'settings' ? 'stats' : screen === 'gifts-catalog' ? 'gifts' : 'menu');
    backButton.onClick(handleBack);
    backButton.show();
    return () => {
      backButton.offClick(handleBack);
      backButton.hide();
    };
  }, [loading, screen]);

  useEffect(() => {
    let active = true;
    let retryTimer = 0;
    let telegramTimer = 0;
    let sdkRetryScript: HTMLScriptElement | null = null;
    let telegramChecks = 0;
    let failedAttempts = 0;
    const authenticate = async (initData: string) => {
      try {
        const access = await authenticateTelegram(initData);
        if (!active) return;
        if (!access) {
          setAuthState('invalid');
          return;
        }
        if (access.accessAllowed) {
          void preloadBoardSnapshot(initData).catch(() => undefined);
          void preloadStatistics(initData).catch(() => undefined);
          void preloadRatingRewards(initData).catch(() => undefined);
          const telegram = getTelegramWebApp();
          const avatarUrl = telegram?.initDataUnsafe?.user?.photo_url;
          if (avatarUrl && avatarUrl.startsWith('https://')) {
            const avatar = new Image();
            avatar.src = avatarUrl;
          }
        }
        setAppAccess(access);
        setAuthState('authorized');
      } catch (error) {
        if (!active) return;
        if (error instanceof TelegramAuthenticationError) {
          setAuthState('invalid');
          return;
        }
        failedAttempts += 1;
        const delay = Math.min(4000, 500 * 2 ** Math.min(failedAttempts - 1, 3));
        retryTimer = window.setTimeout(() => void authenticate(initData), delay);
      }
    };
    const retryTelegramSdk = () => {
      if (sdkRetryScript || getTelegramWebApp()) return;
      const script = document.createElement('script');
      sdkRetryScript = script;
      script.src = `https://telegram.org/js/telegram-web-app.js?retry=${Date.now()}`;
      script.onload = script.onerror = () => {
        script.remove();
        sdkRetryScript = null;
        initializeTelegram();
      };
      document.head.appendChild(script);
    };
    const initializeTelegram = () => {
      if (!active) return;
      const telegram = getTelegramWebApp();
      if (!telegram?.initData) {
        // Some Telegram clients expose WebApp data shortly after the page has
        // mounted. Keep waiting instead of permanently denying this session,
        // and retry the external SDK if its initial request failed.
        telegramChecks += 1;
        if (!telegram && telegramChecks % 20 === 0) retryTelegramSdk();
        telegramTimer = window.setTimeout(initializeTelegram, 250);
        return;
      }
      telegram.ready();
      // This game uses the whole Mini App surface for drawing and panning.
      // Prevent Telegram's vertical swipe gesture from minimizing/closing it.
      // The method is optional because older Telegram clients do not expose it.
      telegram.disableVerticalSwipes?.();
      telegram.expand();
      void authenticate(telegram.initData);
    };
    initializeTelegram();
    return () => {
      active = false;
      window.clearTimeout(retryTimer);
      window.clearTimeout(telegramTimer);
      sdkRetryScript?.remove();
      sdkRetryScript = null;
    };
  }, []);

  useEffect(() => {
    if (authState !== 'authorized') return;
    const initData = getTelegramWebApp()?.initData;
    if (!initData) return;
    let active = true;
    const syncAccess = () => {
      void fetchAppAccess(initData).then((access) => {
        if (!active) return;
        setAppAccess(access);
        if (!access.accessAllowed) setScreen('menu');
      }).catch(() => undefined);
    };
    const timer = window.setInterval(syncAccess, 2500);
    const handleTrophyAwarded = () => syncAccess();
    window.addEventListener('pixelbattle:trophy-awarded', handleTrophyAwarded);
    return () => {
      active = false;
      window.clearInterval(timer);
      window.removeEventListener('pixelbattle:trophy-awarded', handleTrophyAwarded);
    };
  }, [authState]);

  useEffect(() => {
    const requireCaptcha = () => setAppAccess((current) => current ? { ...current, captchaRequired: true } : current);
    window.addEventListener('pixelbattle:captcha-required', requireCaptcha);
    return () => window.removeEventListener('pixelbattle:captcha-required', requireCaptcha);
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
    const rewardsReady = appAccess?.accessAllowed && telegram?.initData
      ? preloadRatingRewards(telegram.initData).catch(() => undefined)
      : Promise.resolve();
    void Promise.all([minimumSplash, preloadParallaxBackground(), preloadGiftAssets(), rewardsReady]).then(() => {
      if (active) setLoading(false);
    }).catch(() => {
      // Keep the loading screen visible if a required layer could not be loaded.
    });
    return () => {
      active = false;
      window.clearTimeout(timer);
    };
  }, [appAccess?.accessAllowed, authState]);

  const handleItemRewardClaimed = (rewardId: number) => {
    setAppAccess((current) => current ? {
      ...current,
      pendingItemRewards: current.pendingItemRewards.filter((reward) => reward.rewardId !== rewardId),
    } : current);
    const initData = getTelegramWebApp()?.initData;
    if (!initData) return;
    void fetchAppAccess(initData).then(setAppAccess).catch(() => undefined);
  };

  if (authState !== 'authorized') return <main className="app-shell"><section className="phone-frame"><LoadingScreen message={authState === 'invalid' ? 'Ошибка проверки Telegram' : 'Откройте через Telegram'} /></section></main>;
  return <main className="app-shell"><section className="phone-frame" aria-label="Pixel Battle">
    {loading ? <LoadingScreen /> : <>
      <MainMenu active={maintenanceMode || screen === 'menu'} maintenance={maintenanceMode} onOpenMap={() => { if (!maintenanceMode) setScreen('map'); }} onOpenStats={() => { if (!maintenanceMode) setScreen('stats'); }} onOpenGifts={() => { if (!maintenanceMode) setScreen('gifts'); }} />
      {!maintenanceMode && screen !== 'menu' && (screen === 'stats' ? <StatisticsScreen onBack={() => setScreen('menu')} onOpenRating={() => setScreen('rating')} onOpenAgreement={() => setScreen('agreement')} onOpenSettings={() => setScreen('settings')} /> : screen === 'rating' ? <RatingScreen onBack={() => setScreen('stats')} /> : screen === 'agreement' ? <AgreementScreen onBack={() => setScreen('stats')} /> : screen === 'settings' ? <SettingsScreen onBack={() => setScreen('stats')} /> : screen === 'gifts' ? <GiftsScreen prizes={appAccess?.prizes ?? []} pendingItemRewards={appAccess?.pendingItemRewards ?? []} onItemRewardClaimed={handleItemRewardClaimed} onOpenCatalog={() => setScreen('gifts-catalog')} onBack={() => setScreen('menu')} /> : screen === 'gifts-catalog' ? <GiftsCatalogScreen prizes={appAccess?.prizes ?? []} soldOutTrophies={appAccess?.soldOutTrophies ?? []} onBack={() => setScreen('gifts')} /> : <BattleScreen online={appAccess?.online ?? null} />)}
    </>}
    {!loading && !maintenanceMode && <QuestNotifications active={screen === 'map'} />}
    <CaptchaOverlay
      active={appAccess?.captchaRequired === true}
      onSolved={() => setAppAccess((current) => current ? { ...current, captchaRequired: false } : current)}
    />
  </section></main>;
}

import { useEffect, useRef, useState, type CSSProperties } from 'react';
import { currentDailyKey, getDailyQuests } from '../dailyQuests';
import { getPlayerLevelProgress } from '../playerLevel';
import { getCachedStatistics, refreshStatistics } from '../statisticsCache';
import { isCurrentUserNickname, shouldPlayRewardSound } from '../rewardSound';

type QuestNotice = { id: string; label: string; strike: boolean; celebrate: boolean; ownReward: boolean };
type TrophyAwardedDetail = { nickname: string; text: string };

const CONFETTI_COLORS = ['#008EFB', '#FFD60A', '#31E52D', '#FF3B30', '#AF52DE', '#FFFFFF'];
const CONFETTI = Array.from({ length: 42 }, (_, index) => ({
  left: `${(index * 37 + 7) % 100}%`,
  delay: `${(index % 7) * 0.08}s`,
  duration: `${1.9 + (index % 5) * 0.16}s`,
  drift: `${((index * 29) % 81) - 40}px`,
  color: CONFETTI_COLORS[index % CONFETTI_COLORS.length],
}));

export function QuestNotifications({ active: mapActive }: { active: boolean }) {
  const [notices, setNotices] = useState<QuestNotice[]>([]);
  const activeRef = useRef(mapActive);
  activeRef.current = mapActive;
  const completedRef = useRef(new Set<string>());
  const initializedRef = useRef(false);
  const refreshTimerRef = useRef<number | null>(null);
  const refreshUntilRef = useRef(0);
  const trophyNoticeSequenceRef = useRef(0);
  const playerLevelRef = useRef<number | null>(null);

  useEffect(() => {
    const initData = window.Telegram?.WebApp?.initData;
    if (!initData) return;
    const userId = window.Telegram?.WebApp?.initDataUnsafe?.user?.id;

    const sync = async (mayNotify: boolean) => {
      try {
        const stats = await refreshStatistics(initData);
        const quests = getDailyQuests(stats, userId);
        const playerLevel = getPlayerLevelProgress(stats.placedPixels + (stats.bonusExperience ?? 0)).level;
        const doneNow = new Set(quests.filter((quest) => quest.done).map((quest) => quest.id));
        if (initializedRef.current && mayNotify && activeRef.current) {
          const newNotices: QuestNotice[] = quests
            .filter((quest) => quest.done && !completedRef.current.has(quest.id))
            .map(({ id, label }) => ({ id, label, strike: true, celebrate: false, ownReward: true }));
          if (playerLevelRef.current !== null && playerLevel > playerLevelRef.current) {
            newNotices.push({ id: `level-${playerLevel}`, label: `Вы достигли ${playerLevel} уровня!`, strike: false, celebrate: false, ownReward: true });
          }
          if (newNotices.length) setNotices((current) => [...current, ...newNotices]);
        }
        completedRef.current = doneNow;
        playerLevelRef.current = playerLevel;
        initializedRef.current = true;
      } catch {
        // A later placement retry will synchronize progress again.
      }
    };

    const cached = getCachedStatistics();
    if (cached) {
      completedRef.current = new Set(getDailyQuests(cached, userId).filter((quest) => quest.done).map((quest) => quest.id));
      playerLevelRef.current = getPlayerLevelProgress(cached.placedPixels + (cached.bonusExperience ?? 0)).level;
      initializedRef.current = true;
    } else {
      void sync(false);
    }

    let disposed = false;
    const poll = async () => {
      await sync(true);
      if (disposed) return;
      if (Date.now() < refreshUntilRef.current) {
        refreshTimerRef.current = window.setTimeout(poll, 120);
      } else {
        refreshTimerRef.current = null;
      }
    };
    const handlePlacement = () => {
      refreshUntilRef.current = Date.now() + 1400;
      if (refreshTimerRef.current === null) refreshTimerRef.current = window.setTimeout(poll, 0);
    };
    const handleTrophyAwarded = (event: Event) => {
      if (!activeRef.current) return;
      const detail = (event as CustomEvent<TrophyAwardedDetail>).detail;
      const label = detail?.text.trim();
      if (!label || typeof detail.nickname !== 'string') return;
      trophyNoticeSequenceRef.current += 1;
      setNotices((current) => [
        ...current,
        { id: `trophy-${Date.now()}-${trophyNoticeSequenceRef.current}`, label, strike: false, celebrate: false, ownReward: isCurrentUserNickname(detail.nickname) },
      ]);
    };
    window.addEventListener('pixelbattle:placement-accepted', handlePlacement);
    window.addEventListener('pixelbattle:trophy-awarded', handleTrophyAwarded);
    return () => {
      disposed = true;
      window.removeEventListener('pixelbattle:placement-accepted', handlePlacement);
      window.removeEventListener('pixelbattle:trophy-awarded', handleTrophyAwarded);
      if (refreshTimerRef.current !== null) window.clearTimeout(refreshTimerRef.current);
      refreshTimerRef.current = null;
    };
  }, []);

  useEffect(() => {
    if (!mapActive) setNotices([]);
  }, [mapActive]);

  const active = notices[0];
  useEffect(() => {
    if (!active) return;
    if (shouldPlayRewardSound(active.ownReward)) {
      const audio = new Audio('/assets/notification.mp3');
      audio.volume = 0.72;
      void audio.play().catch(() => undefined);
    }
    if (active.celebrate) window.Telegram?.WebApp?.HapticFeedback?.notificationOccurred('success');
    const timer = window.setTimeout(() => setNotices((current) => current.slice(1)), 4100);
    return () => window.clearTimeout(timer);
  }, [active]);

  useEffect(() => {
    const timer = window.setInterval(() => {
      const today = currentDailyKey();
      if ([...completedRef.current].some((id) => !id.startsWith(`${today}-`))) completedRef.current.clear();
    }, 60_000);
    return () => window.clearInterval(timer);
  }, []);

  if (!mapActive || !active) return null;
  return (
    <>
      {active.celebrate && <div className="trophy-confetti" aria-hidden="true">
        {CONFETTI.map((particle, index) => (
          <i
            key={index}
            style={{
              left: particle.left,
              animationDelay: particle.delay,
              animationDuration: particle.duration,
              backgroundColor: particle.color,
              '--confetti-drift': particle.drift,
            } as CSSProperties}
          />
        ))}
      </div>}
      <div className="quest-notification-layer" aria-live="polite" aria-atomic="true">
        <section className={`quest-notification${active.strike ? '' : ' no-strike'}`} key={active.id}>
          <svg className="quest-notification-shape" viewBox="0 0 310 72" preserveAspectRatio="none" aria-hidden="true">
            <path
              d="M7 16H24V9C24 4.5 27 2 31 2C33.5 2 35.5 3.2 38 5L66 16H303C306.5 16 308 18.5 308 22V65C308 68.5 306 70 302 70H8C4 70 2 68 2 64V22C2 18 4 16 7 16Z"
              vectorEffect="non-scaling-stroke"
            />
          </svg>
          <span className="quest-notification-text"><span>{active.label}</span></span>
        </section>
      </div>
    </>
  );
}

import { useEffect, useRef, useState } from 'react';
import { currentDailyKey, getDailyQuests } from '../dailyQuests';
import { getCachedStatistics, refreshStatistics } from '../statisticsCache';

type QuestNotice = { id: string; label: string };

export function QuestNotifications() {
  const [notices, setNotices] = useState<QuestNotice[]>([]);
  const completedRef = useRef(new Set<string>());
  const initializedRef = useRef(false);
  const refreshTimerRef = useRef<number | null>(null);
  const refreshUntilRef = useRef(0);

  useEffect(() => {
    const initData = window.Telegram?.WebApp?.initData;
    if (!initData) return;
    const userId = window.Telegram?.WebApp?.initDataUnsafe?.user?.id;

    const sync = async (mayNotify: boolean) => {
      try {
        const stats = await refreshStatistics(initData);
        const quests = getDailyQuests(stats, userId);
        const doneNow = new Set(quests.filter((quest) => quest.done).map((quest) => quest.id));
        if (initializedRef.current && mayNotify) {
          const newNotices = quests
            .filter((quest) => quest.done && !completedRef.current.has(quest.id))
            .map(({ id, label }) => ({ id, label }));
          if (newNotices.length) setNotices((current) => [...current, ...newNotices]);
        }
        completedRef.current = doneNow;
        initializedRef.current = true;
      } catch {
        // A later placement retry will synchronize progress again.
      }
    };

    const cached = getCachedStatistics();
    if (cached) {
      completedRef.current = new Set(getDailyQuests(cached, userId).filter((quest) => quest.done).map((quest) => quest.id));
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
    window.addEventListener('pixelbattle:placement-accepted', handlePlacement);
    return () => {
      disposed = true;
      window.removeEventListener('pixelbattle:placement-accepted', handlePlacement);
      if (refreshTimerRef.current !== null) window.clearTimeout(refreshTimerRef.current);
      refreshTimerRef.current = null;
    };
  }, []);

  const active = notices[0];
  useEffect(() => {
    if (!active) return;
    const audio = new Audio('/assets/notification.mp3');
    audio.volume = 0.72;
    void audio.play().catch(() => undefined);
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

  if (!active) return null;
  return (
    <div className="quest-notification-layer" aria-live="polite" aria-atomic="true">
      <section className="quest-notification" key={active.id}>
        <svg className="quest-notification-shape" viewBox="0 0 310 72" preserveAspectRatio="none" aria-hidden="true">
          <path
            d="M7 16H24V9C24 4.5 27 2 31 2C33.5 2 35.5 3.2 38 5L66 16H303C306.5 16 308 18.5 308 22V65C308 68.5 306 70 302 70H8C4 70 2 68 2 64V22C2 18 4 16 7 16Z"
            vectorEffect="non-scaling-stroke"
          />
        </svg>
        <span className="quest-notification-text"><span>{active.label}</span></span>
      </section>
    </div>
  );
}

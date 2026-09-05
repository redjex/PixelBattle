import { useEffect, useMemo, useState } from 'react';
import { getCachedStatistics, refreshStatistics, type PlayerStatistics } from '../statisticsCache';
import { currentDailyKey, getDailyQuests } from '../dailyQuests';
import { getLevelReward, getPlayerLevelProgress } from '../playerLevel';

type LoadState = 'loading' | 'ready' | 'error';
type Props = { onBack: () => void; onOpenAgreement: () => void; onOpenRating: () => void };

const emptyStats: PlayerStatistics = {
  placedPixels: 0,
  repaintedPixels: 0,
  currentPixels: 0,
  dailyPlacedPixels: 0,
  dailyRepaintedPixels: 0,
  dailyColorsUsed: 0,
  dailyUniqueCells: 0,
};
function CasinoStatNumber({ value }: { value: number }) {
  const text = String(Math.max(0, Math.floor(value)));
  return (
    <span className="casino-number stats-casino-number" aria-label={text}>
      {text.split('').map((character, index) => {
        const digit = Number(character);
        const sequence = Array.from({ length: 7 }, (_, step) => (digit + step + 3) % 10).concat(digit);
        return (
          <span className="casino-digit" key={`${text}-${index}`} aria-hidden="true">
            <i style={{ animationDelay: `${index * 35}ms` }}>
              {sequence.map((number, step) => <b key={step}>{number}</b>)}
            </i>
          </span>
        );
      })}
    </span>
  );
}

export function StatisticsScreen({ onBack, onOpenAgreement, onOpenRating }: Props) {
  const initialStatistics = getCachedStatistics();
  const [stats, setStats] = useState<PlayerStatistics>(initialStatistics ?? emptyStats);
  const [loadState, setLoadState] = useState<LoadState>(initialStatistics ? 'ready' : 'loading');
  const [reloadNonce, setReloadNonce] = useState(0);
  const [dailyKey, setDailyKey] = useState(currentDailyKey);

  useEffect(() => {
    let currentDay = dailyKey;
    const timer = window.setInterval(() => {
      const nextDay = currentDailyKey();
      if (nextDay === currentDay) return;
      currentDay = nextDay;
      setDailyKey(nextDay);
      setReloadNonce((value) => value + 1);
    }, 60_000);
    return () => window.clearInterval(timer);
  }, [dailyKey]);

  useEffect(() => {
    const initData = window.Telegram?.WebApp?.initData;
    if (!initData) { setLoadState('error'); return; }
    const controller = new AbortController();
    const hasCachedStatistics = getCachedStatistics() !== null;
    if (!hasCachedStatistics) setLoadState('loading');
    refreshStatistics(initData, controller.signal)
      .then((result) => { setStats(result); setLoadState('ready'); })
      .catch((error: unknown) => {
        if (!(error instanceof DOMException && error.name === 'AbortError') && !hasCachedStatistics) setLoadState('error');
      });
    return () => controller.abort();
  }, [reloadNonce]);

  const user = window.Telegram?.WebApp?.initDataUnsafe?.user;
  const username = user?.username ? `@${user.username}` : user?.first_name || 'Игрок';
  const avatar = user?.photo_url;
  const { level, progress } = getPlayerLevelProgress(stats.placedPixels);
  const currentLevelReward = getLevelReward(level);
  const nextLevelReward = level < 100 ? getLevelReward(level + 1) : null;
  const [displayedProgress, setDisplayedProgress] = useState(0);
  useEffect(() => {
    setDisplayedProgress(0);
    let secondFrame = 0;
    const firstFrame = window.requestAnimationFrame(() => {
      secondFrame = window.requestAnimationFrame(() => setDisplayedProgress(progress));
    });
    return () => {
      window.cancelAnimationFrame(firstFrame);
      window.cancelAnimationFrame(secondFrame);
    };
  }, [progress]);
  const visibleQuests = useMemo(() => getDailyQuests(stats, user?.id, dailyKey), [dailyKey, stats, user?.id]);

  return (
    <div className="stats-screen">
      <section className="stats-content stats-redesign" aria-label="Статистика игрока">
        {loadState === 'loading' ? <p className="stats-status">Загружаем статистику…</p> : loadState === 'error' ? (
          <div className="stats-status"><p>Не удалось загрузить статистику.</p><button onClick={() => setReloadNonce((value) => value + 1)}>Повторить</button></div>
        ) : (
          <>
            <img className="stats-logo" src="/assets/stats.png" alt="Статистика" />
            <section className="stats-profile">
              {avatar ? <img className="stats-avatar" src={avatar} alt="" /> : <div className="stats-avatar stats-avatar-fallback">{username.slice(0, 1).toUpperCase()}</div>}
              <strong className="stats-username">{username}</strong>
              <button className="stats-level-button" onClick={onOpenRating} aria-label="Открыть рейтинг и награды">
                <img
                  className="stats-level-hint-icon"
                  src={currentLevelReward.item === 'bomb' ? '/assets/bomb.svg' : '/assets/ice.svg'}
                  alt=""
                />
                <span className="stats-level-progress" aria-label={`Прогресс до уровня ${Math.min(100, level + 1)}`}>
                  <span>{level}</span>
                  <span className="stats-progress">
                    <em style={{ width: `${displayedProgress * 100}%` }} />
                    <i style={{ left: `${displayedProgress * 100}%` }}><b /></i>
                  </span>
                  <span>{level >= 100 ? 'MAX' : Math.min(100, level + 1)}</span>
                </span>
                {nextLevelReward
                  ? <img className="stats-level-hint-icon" src={nextLevelReward.item === 'bomb' ? '/assets/bomb.svg' : '/assets/ice.svg'} alt="" />
                  : <span className="stats-level-hint-icon" aria-hidden="true" />}
              </button>
            </section>
            <div className="stats-top-cards">
              <article className="stats-pixel-card"><strong><CasinoStatNumber value={stats.repaintedPixels} /></strong><span>Пикселей перекрашено</span></article>
              <article className="stats-pixel-card"><strong><CasinoStatNumber value={stats.currentPixels} /></strong><span>Твоих пикселей на карте</span></article>
            </div>
            <section className="stats-quests">{visibleQuests.map((quest) => <div key={quest.id} className={quest.done ? 'quest done' : 'quest'}><span>{quest.label}</span><strong>{quest.completed} / {quest.target}</strong></div>)}</section>
        <button className="stats-agreement" onClick={onOpenAgreement}>Политика конфиденциальности</button>
          </>
        )}
      </section>
      <button className="stats-back" onClick={onBack}>Назад</button>
    </div>
  );
}

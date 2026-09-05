import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { getCachedStatistics } from '../statisticsCache';
import { getLevelReward, getPlayerLevelProgress } from '../playerLevel';

type Props = { onBack: () => void };
type RewardState = {
  currentLevel: number;
  claimedLevels: number[];
  inventory: { bombs: number; ice: number; freezeRemaining: number };
};

const levels = Array.from({ length: 100 }, (_, index) => 100 - index);
let cachedRatingRewards: RewardState | null = null;
let ratingRewardsRequest: Promise<RewardState> | null = null;

function rewardsUrl() {
  const apiUrl = import.meta.env.VITE_API_URL ?? window.location.origin;
  return `${apiUrl}/api/boards/main/rewards`;
}

export function preloadRatingRewards(initData: string) {
  if (cachedRatingRewards) return Promise.resolve(cachedRatingRewards);
  if (!ratingRewardsRequest) {
    ratingRewardsRequest = fetch(rewardsUrl(), { cache: 'no-store', headers: { 'X-Telegram-Init-Data': initData } })
      .then((response) => {
        if (!response.ok) throw new Error(`Rewards request failed: ${response.status}`);
        return response.json() as Promise<RewardState>;
      })
      .then((result) => {
        cachedRatingRewards = result;
        return result;
      })
      .finally(() => { ratingRewardsRequest = null; });
  }
  return ratingRewardsRequest;
}

export function RatingScreen({ onBack }: Props) {
  const statistics = getCachedStatistics();
  const calculated = getPlayerLevelProgress(statistics?.placedPixels ?? 0);
  const [rewardState, setRewardState] = useState<RewardState | null>(cachedRatingRewards);
  const [status, setStatus] = useState<'loading' | 'ready' | 'error'>(cachedRatingRewards ? 'ready' : 'loading');
  const [claimingLevel, setClaimingLevel] = useState<number | null>(null);
  const scrollRef = useRef<HTMLElement>(null);
  const currentRowRef = useRef<HTMLDivElement>(null);
  const currentLevel = rewardState?.currentLevel ?? calculated.level;
  const claimed = useMemo(() => new Set(rewardState?.claimedLevels ?? []), [rewardState?.claimedLevels]);
  const user = window.Telegram?.WebApp?.initDataUnsafe?.user;
  const avatar = user?.photo_url;
  const playerName = user?.username ? `@${user.username}` : user?.first_name || 'Игрок';
  const trackProgress = Math.max(0, Math.min(1, (currentLevel - 1 + calculated.progress) / 99));

  const loadRewards = () => {
    const initData = window.Telegram?.WebApp?.initData;
    if (!initData) { setStatus('error'); return; }
    setStatus('loading');
    preloadRatingRewards(initData)
      .then((result) => { setRewardState(result); setStatus('ready'); })
      .catch(() => setStatus('error'));
  };

  useEffect(() => {
    if (!cachedRatingRewards) loadRewards();
  }, []);
  useLayoutEffect(() => {
    if (status !== 'ready') return;
    const scroller = scrollRef.current;
    const currentRow = currentRowRef.current;
    if (!scroller || !currentRow) return;
    scroller.scrollTop = Math.max(0, currentRow.offsetTop + currentRow.offsetHeight / 2 - scroller.clientHeight / 2);
  }, [currentLevel, status]);

  const claimReward = async (level: number) => {
    if (level > currentLevel || claimed.has(level) || claimingLevel !== null) return;
    const initData = window.Telegram?.WebApp?.initData;
    if (!initData) return;
    setClaimingLevel(level);
    try {
      const response = await fetch(rewardsUrl(), {
        method: 'POST',
        cache: 'no-store',
        headers: { 'Content-Type': 'application/json', 'X-Telegram-Init-Data': initData },
        body: JSON.stringify({ level }),
      });
      if (!response.ok) throw new Error(`Reward claim failed: ${response.status}`);
      const result = await response.json() as RewardState;
      cachedRatingRewards = result;
      setRewardState(result);
    } catch {
      setStatus('error');
    } finally {
      setClaimingLevel(null);
    }
  };

  return (
    <div className="rating-screen">
      <img className="rating-logo" src="/assets/raiting.png" alt="Рейтинг" />
      {status === 'error' ? (
        <div className="rating-status"><p>Не удалось загрузить награды.</p><button onClick={loadRewards}>Повторить</button></div>
      ) : status === 'loading' ? (
        <div className="rating-status"><p>Загружаем награды…</p></div>
      ) : (
        <section ref={scrollRef} className="rating-scroll" aria-label="Награды за уровни">
          <div className="rating-track-area">
            <div className="rating-track"><i style={{ height: `${trackProgress * 100}%` }} /><b style={{ bottom: `${trackProgress * 100}%` }} /></div>
            {levels.map((level) => {
              const reward = getLevelReward(level);
              const isClaimed = claimed.has(level);
              const isAvailable = level <= currentLevel && !isClaimed;
              return (
                <div className={`rating-row${level === currentLevel ? ' current' : ''}`} key={level} ref={level === currentLevel ? currentRowRef : undefined}>
                  <div className="rating-level-side">
                    {level === currentLevel && (avatar
                      ? <img className="rating-avatar" src={avatar} alt={playerName} />
                      : <span className="rating-avatar rating-avatar-fallback">{playerName.slice(0, 1).toUpperCase()}</span>)}
                    <span className="rating-level-number">{level}</span>
                  </div>
                  <button
                    className={`rating-reward${isAvailable ? ' available' : ''}${isClaimed ? ' claimed' : ''}`}
                    type="button"
                    disabled={!isAvailable || claimingLevel !== null}
                    onClick={() => void claimReward(level)}
                    aria-label={`${isClaimed ? 'Получено' : isAvailable ? 'Забрать' : 'Награда'}: ${reward.amount} ${reward.item === 'bomb' ? 'бомба' : 'заморозка'}, уровень ${level}`}
                  >
                    <span><img src={reward.item === 'bomb' ? '/assets/bomb.svg' : '/assets/ice.svg'} alt="" /><b>{reward.amount}</b></span>
                  </button>
                </div>
              );
            })}
          </div>
        </section>
      )}
      <button className="stats-back rating-back" onClick={onBack}>Назад</button>
    </div>
  );
}

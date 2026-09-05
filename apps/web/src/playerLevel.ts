export function getPlayerLevelProgress(placedPixels: number) {
  const questCount = Math.max(0, Math.floor(placedPixels / 10));
  const level = Math.min(100, Math.floor(Math.log2(questCount + 1)) + 1);
  const currentLevelStart = level <= 1 ? 0 : 2 ** (level - 1) - 1;
  const nextLevelTarget = level >= 100 ? currentLevelStart : 2 ** level - 1;
  const progress = level >= 100
    ? 1
    : Math.min(1, (questCount - currentLevelStart) / Math.max(1, nextLevelTarget - currentLevelStart));
  return { level, progress };
}

export function getLevelReward(level: number) {
  return {
    item: level % 2 === 0 ? 'ice' as const : 'bomb' as const,
    amount: (Math.floor((level - 1) / 20) + 1) * 5,
  };
}

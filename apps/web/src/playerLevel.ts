export function pixelsRequiredForLevel(level: number) {
  const normalizedLevel = Math.max(1, Math.min(100, Math.floor(level)));
  return 5 * (normalizedLevel - 1) * normalizedLevel;
}

export function getPlayerLevelProgress(placedPixels: number) {
  const normalizedPlacedPixels = Math.max(0, Math.floor(placedPixels));
  const level = Math.min(100, Math.max(1, Math.floor((1 + Math.sqrt(1 + 0.8 * normalizedPlacedPixels)) / 2)));
  const currentLevelStart = pixelsRequiredForLevel(level);
  const nextLevelTarget = level >= 100 ? currentLevelStart : pixelsRequiredForLevel(level + 1);
  const progress = level >= 100
    ? 1
    : Math.min(1, (normalizedPlacedPixels - currentLevelStart) / Math.max(1, nextLevelTarget - currentLevelStart));
  const pixelsToNextLevel = level >= 100
    ? 0
    : Math.max(0, nextLevelTarget - normalizedPlacedPixels);
  return { level, progress, pixelsToNextLevel };
}

export function getLevelReward(level: number) {
  return {
    item: level % 2 === 0 ? 'ice' as const : 'bomb' as const,
    amount: (Math.floor((level - 1) / 20) + 1) * 5,
  };
}

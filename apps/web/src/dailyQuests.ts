import type { PlayerStatistics } from './statisticsCache';

type DailyMetric = 'dailyPlacedPixels' | 'dailyRepaintedPixels' | 'dailyColorsUsed' | 'dailyUniqueCells';

const QUEST_DEFINITIONS: ReadonlyArray<{
  id: string;
  metric: DailyMetric;
  targets: readonly number[];
  label: (target: number) => string;
}> = [
  { id: 'place', metric: 'dailyPlacedPixels', targets: [10, 20, 30, 50], label: (target) => `Закрасить ${target} пикселей` },
  { id: 'repaint', metric: 'dailyRepaintedPixels', targets: [5, 10, 15, 25], label: (target) => `Закрасить ${target} чужих пикселей` },
  { id: 'colors', metric: 'dailyColorsUsed', targets: [2, 3, 4, 5], label: (target) => `Закрасить пиксели ${target} разными цветами` },
  { id: 'cells', metric: 'dailyUniqueCells', targets: [10, 20, 30, 40], label: (target) => `Закрасить ${target} разных клеток` },
];

function hashSeed(value: string) {
  let hash = 2166136261;
  for (let index = 0; index < value.length; index += 1) {
    hash ^= value.charCodeAt(index);
    hash = Math.imul(hash, 16777619);
  }
  return hash >>> 0;
}

export function currentDailyKey() {
  return new Date().toISOString().slice(0, 10);
}

export function getDailyQuests(stats: PlayerStatistics, userId: number | string | undefined, dailyKey = currentDailyKey()) {
  const userSeed = `${dailyKey}:${userId ?? 'guest'}`;
  return QUEST_DEFINITIONS
    .map((definition) => {
      const target = definition.targets[hashSeed(`${userSeed}:${definition.id}`) % definition.targets.length];
      const completed = Math.min(stats[definition.metric], target);
      return {
        id: `${dailyKey}-${definition.id}`,
        label: definition.label(target),
        target,
        completed,
        done: completed >= target,
        order: hashSeed(`${userSeed}:order:${definition.id}`),
      };
    })
    .sort((left, right) => left.order - right.order)
    .slice(0, 3);
}

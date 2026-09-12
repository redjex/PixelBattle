import { useState } from 'react';
import { getRewardSoundMode, setRewardSoundMode, type RewardSoundMode } from '../rewardSound';
import { getTemplateOpacity, setTemplateOpacity, type TemplateOpacity } from '../templateOpacity';


type Props = { onBack: () => void };

const soundOptions: Array<{ mode: RewardSoundMode; label: string }> = [
  { mode: 'mine', label: 'Только для меня' },
  { mode: 'all', label: 'Для всех' },
  { mode: 'off', label: 'Выключить' },
];

const opacityOptions: TemplateOpacity[] = [20, 50, 100];

export function SettingsScreen({ onBack }: Props) {
  const [soundMode, setSoundMode] = useState(getRewardSoundMode);
  const [opacity, setOpacity] = useState(getTemplateOpacity);

  const selectSoundMode = (mode: RewardSoundMode) => {
    setRewardSoundMode(mode);
    setSoundMode(mode);
  };

  const selectOpacity = (value: TemplateOpacity) => {
    setTemplateOpacity(value);
    setOpacity(value);
  };

  return (
    <div className="settings-screen">
      <img className="settings-logo" src="/assets/settings.png" alt="Настройки" />
      <div className="settings-content">
        <section className="settings-group" aria-labelledby="reward-sound-title">
          <h2 id="reward-sound-title">Звук наград</h2>
          <div className="settings-sound-options" role="radiogroup" aria-label="Когда воспроизводить звук награды">
            {soundOptions.map(({ mode, label }) => (
              <button
                key={mode}
                type="button"
                className={`settings-sound-option${soundMode === mode ? ' selected' : ''}`}
                role="radio"
                aria-checked={soundMode === mode}
                onClick={() => selectSoundMode(mode)}
              >
                <span className="settings-radio" aria-hidden="true" />
                <span>{label}</span>
              </button>
            ))}
          </div>
        </section>
        <section className="settings-group" aria-labelledby="template-opacity-title">
          <h2 id="template-opacity-title">Видимость шаблона</h2>
          <div className="settings-sound-options settings-opacity-options" role="radiogroup" aria-label="Видимость изображения-шаблона">
            {opacityOptions.map((value) => (
              <button
                key={value}
                type="button"
                className={`settings-sound-option${opacity === value ? ' selected' : ''}`}
                role="radio"
                aria-checked={opacity === value}
                onClick={() => selectOpacity(value)}
              >
                <span className="settings-radio" aria-hidden="true" />
                <span>{value}%</span>
              </button>
            ))}
          </div>
        </section>
      </div>
      <button className="stats-back" onClick={onBack}>Назад</button>
    </div>
  );
}

import { useEffect, useRef, useState } from 'react';
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
  const [hideUsername, setHideUsername] = useState(false);
  const [privacyBusy, setPrivacyBusy] = useState(false);
  const privacyRevisionRef = useRef(0);

  useEffect(() => {
    const initData = window.Telegram?.WebApp?.initData;
    if (!initData) return;
    const controller = new AbortController();
    const revision = privacyRevisionRef.current;
    const apiUrl = import.meta.env.VITE_API_URL ?? window.location.origin;
    void fetch(`${apiUrl}/api/boards/profiles/me/privacy`, {
      cache: 'no-store',
      signal: controller.signal,
      headers: { 'X-Telegram-Init-Data': initData },
    })
      .then((response) => response.ok ? response.json() as Promise<{ hideUsername?: boolean }> : null)
      .then((result) => {
        if (result && privacyRevisionRef.current === revision) setHideUsername(Boolean(result.hideUsername));
      })
      .catch(() => undefined);
    return () => controller.abort();
  }, []);

  const selectSoundMode = (mode: RewardSoundMode) => {
    setRewardSoundMode(mode);
    setSoundMode(mode);
  };

  const selectOpacity = (value: TemplateOpacity) => {
    setTemplateOpacity(value);
    setOpacity(value);
  };

  const toggleUsernamePrivacy = async () => {
    const initData = window.Telegram?.WebApp?.initData;
    if (!initData || privacyBusy) return;
    const nextValue = !hideUsername;
    const previousValue = hideUsername;
    privacyRevisionRef.current += 1;
    setHideUsername(nextValue);
    setPrivacyBusy(true);
    try {
      const apiUrl = import.meta.env.VITE_API_URL ?? window.location.origin;
      const response = await fetch(`${apiUrl}/api/boards/profiles/me/privacy`, {
        method: 'PUT',
        cache: 'no-store',
        headers: { 'Content-Type': 'application/json', 'X-Telegram-Init-Data': initData },
        body: JSON.stringify({ hideUsername: nextValue }),
      });
      if (!response.ok) throw new Error('Failed to save privacy setting');
      const result = await response.json() as { hideUsername?: boolean };
      setHideUsername(Boolean(result.hideUsername));
    } catch {
      setHideUsername(previousValue);
    } finally {
      setPrivacyBusy(false);
    }
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
        <section className="settings-group" aria-labelledby="privacy-title">
          <h2 id="privacy-title">Приватность</h2>
          <button
            type="button"
            className={`settings-sound-option${hideUsername ? ' selected' : ''}`}
            aria-pressed={hideUsername}
            aria-busy={privacyBusy}
            onClick={() => void toggleUsernamePrivacy()}
          >
            <span className="settings-radio" aria-hidden="true" />
            <span>Скрыть юзернейм</span>
          </button>
        </section>
      </div>
      <button className="stats-back" onClick={onBack}>Назад</button>
    </div>
  );
}

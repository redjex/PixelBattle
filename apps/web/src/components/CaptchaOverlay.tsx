import { useEffect, useRef, useState, type FormEvent } from 'react';

type CaptchaPayload = {
  verified?: boolean;
  required?: boolean;
  challengeId?: string;
  image?: string;
};

export function CaptchaOverlay({ active, onSolved }: { active: boolean; onSolved: () => void }) {
  const [challenge, setChallenge] = useState<{ id: string; image: string } | null>(null);
  const [answer, setAnswer] = useState('');
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);

  const applyPayload = (payload: CaptchaPayload) => {
    if (payload.required === false || payload.verified === true) {
      onSolved();
      return;
    }
    if (payload.required && payload.challengeId && payload.image?.startsWith('data:image/png;base64,')) {
      setChallenge({ id: payload.challengeId, image: payload.image });
      window.setTimeout(() => inputRef.current?.focus(), 0);
    }
  };

  const requestChallenge = async () => {
    const initData = window.Telegram?.WebApp?.initData;
    if (!initData) return;
    setBusy(true);
    setFailed(false);
    try {
      const apiUrl = import.meta.env.VITE_API_URL ?? window.location.origin;
      const response = await fetch(`${apiUrl}/api/boards/captcha`, {
        cache: 'no-store',
        headers: { 'X-Telegram-Init-Data': initData },
      });
      if (!response.ok) throw new Error('captcha unavailable');
      applyPayload(await response.json() as CaptchaPayload);
    } catch {
      setFailed(true);
    } finally {
      setBusy(false);
    }
  };

  useEffect(() => {
    if (!active) {
      setChallenge(null);
      setAnswer('');
      setFailed(false);
      return;
    }
    void requestChallenge();
  }, [active]);

  const verify = async (event: FormEvent) => {
    event.preventDefault();
    const initData = window.Telegram?.WebApp?.initData;
    if (!initData || !challenge || answer.length !== 5 || busy) return;
    setBusy(true);
    setFailed(false);
    try {
      const apiUrl = import.meta.env.VITE_API_URL ?? window.location.origin;
      const response = await fetch(`${apiUrl}/api/boards/captcha`, {
        method: 'POST',
        cache: 'no-store',
        headers: { 'Content-Type': 'application/json', 'X-Telegram-Init-Data': initData },
        body: JSON.stringify({ challengeId: challenge.id, answer }),
      });
      if (!response.ok) throw new Error('captcha unavailable');
      const payload = await response.json() as CaptchaPayload;
      if (payload.verified !== true) {
        setAnswer('');
        setFailed(true);
      }
      applyPayload(payload);
    } catch {
      setFailed(true);
    } finally {
      setBusy(false);
    }
  };

  if (!active) return null;
  return (
    <div className="captcha-backdrop" role="presentation">
      <section
        className="captcha-card"
        data-captcha-version="5"
        role="dialog"
        aria-modal="true"
        aria-labelledby="captcha-title"
      >
        <h2 id="captcha-title">Подтвердите, что вы человек</h2>
        <p>Введите пять символов с картинки</p>
        {challenge
          ? <img className="captcha-image" src={challenge.image} alt="Код проверки" />
          : <div className="captcha-image captcha-placeholder" aria-hidden="true" />}
        <form onSubmit={(event) => void verify(event)}>
          <input
            ref={inputRef}
            value={answer}
            onChange={(event) => setAnswer(event.target.value.toUpperCase().replace(/[^A-Z0-9]/g, '').slice(0, 5))}
            inputMode="text"
            autoCapitalize="characters"
            autoComplete="off"
            autoCorrect="off"
            spellCheck={false}
            maxLength={5}
            aria-label="Символы с картинки"
            disabled={!challenge || busy}
          />
          <button type="submit" disabled={!challenge || answer.length !== 5 || busy}>Продолжить</button>
        </form>
        {failed && <button className="captcha-retry" type="button" onClick={() => void requestChallenge()}>Попробовать ещё раз</button>}
      </section>
    </div>
  );
}

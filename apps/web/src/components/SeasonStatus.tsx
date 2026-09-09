type Props = {
  online?: number | null;
  showOnline?: boolean;
};

export function SeasonStatus({ online = null, showOnline = false }: Props) {
  return (
    <div className="app-top-status" aria-label={showOnline ? `Сезон 1, онлайн ${online ?? 'загружается'}` : 'Сезон 1'}>
      <div className="brand-pill">Сезон 1</div>
      {showOnline && (
        <div className="online-pill" aria-live="polite">
          <span className="online-dot" aria-hidden="true" />
          <span>{online ?? '—'}</span>
        </div>
      )}
    </div>
  );
}

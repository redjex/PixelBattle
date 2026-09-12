type Props = { onBack: () => void };

export function SettingsScreen({ onBack }: Props) {
  return (
    <div className="settings-screen">
      <h1 className="settings-title">Настройки</h1>
      <p className="settings-placeholder">Настройки приложения появятся здесь.</p>
      <button className="stats-back" onClick={onBack}>Назад</button>
    </div>
  );
}

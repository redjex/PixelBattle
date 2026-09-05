type Props = {
  zoom: number;
  onZoom: (value: number) => void;
  onImageTemplate: () => void;
  hasImageTemplate: boolean;
  onCancelImageTemplate: () => void;
};

const MIN_ZOOM = 0.5;
const MAX_ZOOM = 100;
const BUTTON_ZOOM_FACTOR = 1.08;

export function GlassControls({ zoom, onZoom, onImageTemplate, hasImageTemplate, onCancelImageTemplate }: Props) {
  return (
    <div className="zoom-controls">
      <button className="glass-button image-template-button" onClick={onImageTemplate} aria-label="Загрузить изображение-шаблон">
        <img src="/assets/photo.png" alt="" />
      </button>
      <div className="glass-stack">
        <button onClick={() => onZoom(Math.min(MAX_ZOOM, zoom * BUTTON_ZOOM_FACTOR))} aria-label="Приблизить"><span className="zoom-glyph zoom-plus" aria-hidden="true" /></button>
        <button onClick={() => onZoom(Math.max(MIN_ZOOM, zoom / BUTTON_ZOOM_FACTOR))} aria-label="Отдалить"><span className="zoom-glyph zoom-minus" aria-hidden="true" /></button>
      </div>
      {hasImageTemplate && (
        <button className="glass-button template-cancel-button" onClick={onCancelImageTemplate} aria-label="Убрать изображение-шаблон">
          <span className="zoom-glyph zoom-plus template-close-glyph" aria-hidden="true" />
        </button>
      )}
    </div>
  );
}

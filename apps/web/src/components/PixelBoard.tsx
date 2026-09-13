import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react';
import { usePixelSocket } from '../hooks/usePixelSocket';
import type { Pixel } from '../types/pixel';
import { loadBoardSnapshot } from '../boardSnapshot';
import type { TemplatePlacement } from '../templateStorage';

const DEFAULT_BOARD_SIZE = 150;
const MIN_ZOOM = 0.5;
const MAX_ZOOM = 100;
const ZOOM_SPEED_MULTIPLIER = 1.2;
const TEMPLATE_COLORS = [
  '#FF8080', '#FFCA73', '#FBFFA5', '#7CFF80', '#7EFFF2', '#84D0FF', '#8290FF', '#CD81FF', '#FF80D0', '#FDFDFD',
  '#FF0000', '#FF9D00', '#F2FF00', '#00FF07', '#00FFE6', '#009DFF', '#001EFF', '#9900FF', '#FF00A1', '#8A8A8A',
  '#870000', '#8D4E00', '#B6A700', '#009904', '#009687', '#00568C', '#001194', '#53008A', '#8E005A', '#000000',
].map((color) => ({ color, red: Number.parseInt(color.slice(1, 3), 16), green: Number.parseInt(color.slice(3, 5), 16), blue: Number.parseInt(color.slice(5, 7), 16) }));
const TEMPLATE_NEUTRAL_COLORS = TEMPLATE_COLORS.filter((color) => Math.max(color.red, color.green, color.blue) - Math.min(color.red, color.green, color.blue) <= 8);

const normalizeBoardColor = (color: string) => color.toUpperCase() === '#F8F9FA' ? '#FFFFFF' : color;

type TemplateState = { image: HTMLImageElement; canvas: HTMLCanvasElement; x: number; y: number; width: number; height: number };
type TemplateGesture = { mode: 'move' | 'resize'; pointerId: number; startClientX: number; startClientY: number; startX: number; startY: number; startWidth: number; startHeight: number };
type Props = { color: string; zoom: number; onZoom: (zoom: number) => void; eyedropper: boolean; onPickColor: (color: string) => void; onEyedropperEnd: () => void; paintNonce: number; useIce: boolean; onSelectPixel: (pixel: { x: number; y: number } | null) => void; onInspectPixel: (pixel: Pixel | null) => void; cooldownUntil: number; onPlacementAccepted: (cooldownMs?: number) => void; templateImageUrl: string | null; templatePlacement: TemplatePlacement | null; templateOpacity: number; onTemplatePlacementChange: (placement: TemplatePlacement) => void };

function renderTemplate(image: HTMLImageElement, width: number, height: number) {
  const canvas = document.createElement('canvas');
  canvas.width = width;
  canvas.height = height;
  const context = canvas.getContext('2d', { willReadFrequently: true });
  if (!context) return canvas;

  const sourceCanvas = document.createElement('canvas');
  sourceCanvas.width = image.naturalWidth;
  sourceCanvas.height = image.naturalHeight;
  const sourceContext = sourceCanvas.getContext('2d', { willReadFrequently: true });
  let data: ImageData;

  if (sourceContext) {
    sourceContext.drawImage(image, 0, 0);
    const sourceData = sourceContext.getImageData(0, 0, image.naturalWidth, image.naturalHeight);
    data = context.createImageData(width, height);
    for (let y = 0; y < height; y += 1) {
      const sourceY = Math.min(image.naturalHeight - 1, Math.floor((y + 0.5) * image.naturalHeight / height));
      for (let x = 0; x < width; x += 1) {
        const sourceX = Math.min(image.naturalWidth - 1, Math.floor((x + 0.5) * image.naturalWidth / width));
        const sourceIndex = (sourceY * image.naturalWidth + sourceX) * 4;
        const targetIndex = (y * width + x) * 4;
        data.data[targetIndex] = sourceData.data[sourceIndex];
        data.data[targetIndex + 1] = sourceData.data[sourceIndex + 1];
        data.data[targetIndex + 2] = sourceData.data[sourceIndex + 2];
        data.data[targetIndex + 3] = sourceData.data[sourceIndex + 3];
      }
    }
  } else {
    context.imageSmoothingEnabled = false;
    context.drawImage(image, 0, 0, width, height);
    data = context.getImageData(0, 0, width, height);
  }

  for (let index = 0; index < data.data.length; index += 4) {
    if (data.data[index + 3] < 32) { data.data[index + 3] = 0; continue; }
    const red = data.data[index];
    const green = data.data[index + 1];
    const blue = data.data[index + 2];
    const maximum = Math.max(red, green, blue);
    const chroma = maximum - Math.min(red, green, blue);
    const saturation = maximum > 0 ? chroma / maximum : 0;
    const candidates = chroma <= 24 || saturation <= 0.12 ? TEMPLATE_NEUTRAL_COLORS : TEMPLATE_COLORS;
    let nearest = candidates[0];
    let nearestDistance = Number.POSITIVE_INFINITY;
    for (const candidate of candidates) {
      const redDistance = red - candidate.red;
      const greenDistance = green - candidate.green;
      const blueDistance = blue - candidate.blue;
      const distance = redDistance * redDistance + greenDistance * greenDistance + blueDistance * blueDistance;
      if (distance < nearestDistance) { nearest = candidate; nearestDistance = distance; }
    }
    data.data[index] = nearest.red;
    data.data[index + 1] = nearest.green;
    data.data[index + 2] = nearest.blue;
  }
  context.putImageData(data, 0, 0);
  return canvas;
}

export function PixelBoard({ color, zoom, onZoom, eyedropper, onPickColor, onEyedropperEnd, paintNonce, useIce, onSelectPixel, onInspectPixel, cooldownUntil, onPlacementAccepted, templateImageUrl, templatePlacement, templateOpacity, onTemplatePlacementChange }: Props) {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const checkerPatternRef = useRef<CanvasPattern | null>(null);
  const boardLayerRef = useRef<HTMLCanvasElement | null>(null);
  const boardLayerDirtyRef = useRef(true);
  const pixelsRef = useRef(new Map<string, Pixel>());
  const boardReadyRef = useRef(false);
  const templateRef = useRef<TemplateState | null>(null);
  const templatePlacementRef = useRef(templatePlacement);
  templatePlacementRef.current = templatePlacement;
  const templateGestureRef = useRef<TemplateGesture | null>(null);
  const templateMoveIconRef = useRef<HTMLImageElement | null>(null);
  const templateResizeIconRef = useRef<HTMLImageElement | null>(null);
  const templateWatchIconRef = useRef<HTMLImageElement | null>(null);
  const templateWatchPointerRef = useRef<number | null>(null);
  const templateHoldToShowRef = useRef(false);
  const templatePointerRevealRef = useRef(false);
  const templateSpaceHeldRef = useRef(false);
  const lastTemplateWatchTapRef = useRef(0);
  const boardSizeRef = useRef({ width: DEFAULT_BOARD_SIZE, height: DEFAULT_BOARD_SIZE });
  const panRef = useRef({ x: 0, y: 0 });
  const dragRef = useRef<{ x: number; y: number; cellX: number; cellY: number; moved: boolean; longPressed: boolean; pointerId: number } | null>(null);
  const pointersRef = useRef(new Map<number, { x: number; y: number }>());
  const pinchRef = useRef<{ distance: number; smoothedDistance: number; zoom: number; center: { x: number; y: number }; boardPoint: { x: number; y: number } } | null>(null);
  const pinchFrameRef = useRef<number | null>(null);
  const previousZoomRef = useRef(zoom);
  const renderedZoomRef = useRef(zoom);
  const targetZoomRef = useRef(zoom);
  const wheelAnimationRef = useRef<number | null>(null);
  const viewFrameRef = useRef<number | null>(null);
  const drawRef = useRef<() => void>(() => undefined);
  const canvasPixelRatioRef = useRef(Math.min(
    window.devicePixelRatio || 1,
    window.matchMedia('(pointer: coarse)').matches ? 1.5 : 2,
  ));
  const wheelAnchorRef = useRef<{ x: number; y: number; boardX: number; boardY: number } | null>(null);
  const skipZoomReanchorRef = useRef(false);
  const longPressTimerRef = useRef<number | null>(null);
  const selectedRef = useRef<{ x: number; y: number } | null>(null);
  const [revision, setRevision] = useState(0);
  const [boardReloadNonce, setBoardReloadNonce] = useState(0);
  const [boardDimensions, setBoardDimensions] = useState<{ width: number; height: number } | null>(null);

  const acceptPixel = useCallback((pixel: Pixel) => {
    pixelsRef.current.set(`${pixel.x}:${pixel.y}`, pixel);
    const boardLayer = boardLayerRef.current;
    const boardSize = boardSizeRef.current;
    if (boardLayer && boardLayer.width === boardSize.width && boardLayer.height === boardSize.height && !boardLayerDirtyRef.current) {
      const boardContext = boardLayer.getContext('2d');
      if (boardContext) {
        boardContext.fillStyle = normalizeBoardColor(pixel.color);
        boardContext.fillRect(pixel.x, pixel.y, 1, 1);
      } else {
        boardLayerDirtyRef.current = true;
      }
    } else {
      boardLayerDirtyRef.current = true;
    }
    const selected = selectedRef.current;
    if (selected?.x === pixel.x && selected.y === pixel.y) onInspectPixel(pixel);
    scheduleViewRender();
  }, [onInspectPixel]);
  const reloadBoard = useCallback(() => setBoardReloadNonce((value) => value + 1), []);
  const { place } = usePixelSocket(acceptPixel, reloadBoard);

  useEffect(() => {
    const moveIcon = new Image();
    const resizeIcon = new Image();
    const watchIcon = new Image();
    const refresh = () => setRevision((value) => value + 1);
    moveIcon.onload = refresh;
    resizeIcon.onload = refresh;
    watchIcon.onload = refresh;
    moveIcon.src = '/assets/move.svg';
    resizeIcon.src = '/assets/upscale.svg';
    watchIcon.src = '/assets/watch.svg';
    templateMoveIconRef.current = moveIcon;
    templateResizeIconRef.current = resizeIcon;
    templateWatchIconRef.current = watchIcon;
    return () => {
      moveIcon.onload = null;
      resizeIcon.onload = null;
      watchIcon.onload = null;
      templateMoveIconRef.current = null;
      templateResizeIconRef.current = null;
      templateWatchIconRef.current = null;
    };
  }, []);

  useLayoutEffect(() => {
    const previousZoom = previousZoomRef.current;
    previousZoomRef.current = zoom;
    if (previousZoom === zoom || skipZoomReanchorRef.current) {
      skipZoomReanchorRef.current = false;
      return;
    }
    renderedZoomRef.current = zoom;
    if (wheelAnimationRef.current === null) targetZoomRef.current = zoom;
    const canvas = canvasRef.current;
    if (!canvas) return;
    const rect = canvas.getBoundingClientRect();
    const centerX = rect.width / 2;
    const centerY = rect.height / 2;
    const { width, height } = boardSizeRef.current;
    const previousOriginX = centerX - (width * previousZoom) / 2 + panRef.current.x;
    const previousOriginY = centerY - (height * previousZoom) / 2 + panRef.current.y;
    const boardCenterX = (centerX - previousOriginX) / previousZoom;
    const boardCenterY = (centerY - previousOriginY) / previousZoom;
    const nextOriginX = centerX - boardCenterX * zoom;
    const nextOriginY = centerY - boardCenterY * zoom;
    panRef.current.x = nextOriginX - (centerX - (width * zoom) / 2);
    panRef.current.y = nextOriginY - (centerY - (height * zoom) / 2);
    setRevision((value) => value + 1);
  }, [zoom]);

  useEffect(() => () => {
    if (wheelAnimationRef.current !== null) window.cancelAnimationFrame(wheelAnimationRef.current);
    if (pinchFrameRef.current !== null) window.cancelAnimationFrame(pinchFrameRef.current);
    if (viewFrameRef.current !== null) window.cancelAnimationFrame(viewFrameRef.current);
  }, []);

  useEffect(() => {
    const initData = window.Telegram?.WebApp?.initData;
    if (!initData) return;
    let disposed = false;
    loadBoardSnapshot(initData, boardReloadNonce > 0)
      .then(({ width, height, pixels }) => {
        if (disposed) return;
        boardSizeRef.current = { width, height };
        const nextPixels = new Map(pixels.map((pixel) => [`${pixel.x}:${pixel.y}`, pixel]));
        // Preserve realtime events that arrived after the snapshot request started.
        for (const pixel of pixelsRef.current.values()) {
          if (pixel.version === undefined) continue;
          const key = `${pixel.x}:${pixel.y}`;
          const snapshotPixel = nextPixels.get(key);
          if (!snapshotPixel || (snapshotPixel.version ?? 0) <= pixel.version) nextPixels.set(key, pixel);
        }
        pixelsRef.current = nextPixels;
        boardLayerDirtyRef.current = true;
        boardReadyRef.current = true;
        setBoardDimensions({ width, height });
        setRevision((value) => value + 1);
      })
      .catch((error: unknown) => {
        if (!disposed) console.error('Failed to load board', error);
      });
    return () => { disposed = true; };
  }, [boardReloadNonce]);

  useEffect(() => {
    templateRef.current = null;
    templateGestureRef.current = null;
    templateWatchPointerRef.current = null;
    templateHoldToShowRef.current = false;
    templatePointerRevealRef.current = false;
    templateSpaceHeldRef.current = false;
    lastTemplateWatchTapRef.current = 0;
    setRevision((value) => value + 1);
    if (!templateImageUrl || !boardDimensions) return;
    let disposed = false;
    const image = new Image();
    image.onload = () => {
      if (disposed || !image.naturalWidth || !image.naturalHeight || image.naturalWidth * image.naturalHeight > 100_000_000) return;
      const scale = Math.min(1, boardDimensions.width / image.naturalWidth, boardDimensions.height / image.naturalHeight);
      const width = Math.max(1, Math.floor(image.naturalWidth * scale));
      const height = Math.max(1, Math.floor(image.naturalHeight * scale));
      const saved = templatePlacementRef.current;
      const savedWidth = saved && Number.isFinite(saved.width) ? Math.max(1, Math.min(boardDimensions.width, Math.round(saved.width))) : width;
      const savedHeight = saved && Number.isFinite(saved.height) ? Math.max(1, Math.min(boardDimensions.height, Math.round(saved.height))) : height;
      const x = saved && Number.isFinite(saved.x) ? Math.max(0, Math.min(boardDimensions.width - savedWidth, Math.round(saved.x))) : Math.floor((boardDimensions.width - savedWidth) / 2);
      const y = saved && Number.isFinite(saved.y) ? Math.max(0, Math.min(boardDimensions.height - savedHeight, Math.round(saved.y))) : Math.floor((boardDimensions.height - savedHeight) / 2);
      const canvas = renderTemplate(image, savedWidth, savedHeight);
      templateRef.current = {
        image,
        canvas,
        width: savedWidth,
        height: savedHeight,
        x,
        y,
      };
      onTemplatePlacementChange({ x, y, width: savedWidth, height: savedHeight });
      setRevision((value) => value + 1);
    };
    image.src = templateImageUrl;
    return () => { disposed = true; image.src = ''; };
  }, [templateImageUrl, boardDimensions]);

  useEffect(() => {
    if (Date.now() < cooldownUntil) return;
    const selected = selectedRef.current;
    if (!selected) return;
    const pixel = { ...selected, color };
    void place({ ...pixel, type: 'place_pixel', boardId: 'main', operationId: crypto.randomUUID(), useIce })
      .then((acceptedPixel) => {
        if (!acceptedPixel) return;
        onPlacementAccepted(acceptedPixel.cooldownMs);
        const currentSelection = selectedRef.current;
        if (currentSelection?.x === acceptedPixel.x && currentSelection.y === acceptedPixel.y) onInspectPixel(acceptedPixel);
      });
  }, [paintNonce]);

  useLayoutEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;

    const draw = () => {
      const context = canvas.getContext('2d');
      if (!context) return;
      const rect = canvas.getBoundingClientRect();
      const ratio = canvasPixelRatioRef.current;
      const physicalWidth = Math.round(rect.width * ratio);
      const physicalHeight = Math.round(rect.height * ratio);
      if (canvas.width !== physicalWidth || canvas.height !== physicalHeight) {
        canvas.width = physicalWidth;
        canvas.height = physicalHeight;
      }
      context.setTransform(ratio, 0, 0, ratio, 0, 0);
      if (!checkerPatternRef.current) {
        const tile = document.createElement('canvas');
        tile.width = 16;
        tile.height = 16;
        const tileContext = tile.getContext('2d');
        if (tileContext) {
          tileContext.fillStyle = '#A8A8A8';
          tileContext.fillRect(0, 0, 16, 16);
          tileContext.fillStyle = '#CACACA';
          tileContext.fillRect(8, 0, 8, 8);
          tileContext.fillRect(0, 8, 8, 8);
          checkerPatternRef.current = context.createPattern(tile, 'repeat');
        }
      }
      context.fillStyle = checkerPatternRef.current ?? '#A8A8A8';
      context.fillRect(0, 0, rect.width, rect.height);

      // Do not flash the obsolete placeholder board while the live snapshot loads.
      if (!boardReadyRef.current) return;

      // Pan and zoom are updated together in the gesture frame. Rendering from
      // the React prop here could combine a fresh pan with a one-frame-old zoom
      // and make the board jump, especially when pinching near an edge.
      const cell = renderedZoomRef.current;
      const { width, height } = boardSizeRef.current;
      const originX = rect.width / 2 - (width * cell) / 2 + panRef.current.x;
      const originY = rect.height / 2 - (height * cell) / 2 + panRef.current.y;
      const boardWidth = width * cell;
      const boardHeight = height * cell;
      let boardLayer = boardLayerRef.current;
      if (!boardLayer || boardLayer.width !== width || boardLayer.height !== height) {
        boardLayer = document.createElement('canvas');
        boardLayer.width = width;
        boardLayer.height = height;
        boardLayerRef.current = boardLayer;
        boardLayerDirtyRef.current = true;
      }
      if (boardLayerDirtyRef.current) {
        const boardContext = boardLayer.getContext('2d');
        if (boardContext) {
          boardContext.fillStyle = '#ffffff';
          boardContext.fillRect(0, 0, width, height);
          for (const pixel of pixelsRef.current.values()) {
            boardContext.fillStyle = normalizeBoardColor(pixel.color);
            boardContext.fillRect(pixel.x, pixel.y, 1, 1);
          }
          boardLayerDirtyRef.current = false;
        }
      }
      const visibleLeft = Math.max(0, Math.floor(-originX / cell));
      const visibleTop = Math.max(0, Math.floor(-originY / cell));
      const visibleRight = Math.min(width, Math.ceil((rect.width - originX) / cell));
      const visibleBottom = Math.min(height, Math.ceil((rect.height - originY) / cell));
      const visibleWidth = visibleRight - visibleLeft;
      const visibleHeight = visibleBottom - visibleTop;
      if (visibleWidth > 0 && visibleHeight > 0) {
        context.imageSmoothingEnabled = false;
        context.drawImage(
          boardLayer,
          visibleLeft,
          visibleTop,
          visibleWidth,
          visibleHeight,
          originX + visibleLeft * cell,
          originY + visibleTop * cell,
          visibleWidth * cell,
          visibleHeight * cell,
        );
      }
      context.save();
      context.strokeStyle = '#111111';
      context.lineWidth = 3;
      context.lineCap = 'round';
      context.setLineDash([8, 7]);
      context.lineDashOffset = -3.5;
      const borderX = originX + 1.5;
      const borderY = originY + 1.5;
      const borderWidth = Math.max(0, boardWidth - 3);
      const borderHeight = Math.max(0, boardHeight - 3);
      context.beginPath();
      const horizontalBorderVisible = borderX <= rect.width + 3 && borderX + borderWidth >= -3;
      const verticalBorderVisible = borderY <= rect.height + 3 && borderY + borderHeight >= -3;
      if (horizontalBorderVisible && borderY >= -3 && borderY <= rect.height + 3) {
        context.moveTo(Math.max(-3, borderX), borderY);
        context.lineTo(Math.min(rect.width + 3, borderX + borderWidth), borderY);
      }
      if (horizontalBorderVisible && borderY + borderHeight >= -3 && borderY + borderHeight <= rect.height + 3) {
        context.moveTo(Math.max(-3, borderX), borderY + borderHeight);
        context.lineTo(Math.min(rect.width + 3, borderX + borderWidth), borderY + borderHeight);
      }
      if (verticalBorderVisible && borderX >= -3 && borderX <= rect.width + 3) {
        context.moveTo(borderX, Math.max(-3, borderY));
        context.lineTo(borderX, Math.min(rect.height + 3, borderY + borderHeight));
      }
      if (verticalBorderVisible && borderX + borderWidth >= -3 && borderX + borderWidth <= rect.width + 3) {
        context.moveTo(borderX + borderWidth, Math.max(-3, borderY));
        context.lineTo(borderX + borderWidth, Math.min(rect.height + 3, borderY + borderHeight));
      }
      context.stroke();
      context.restore();
      const template = templateRef.current;
      if (template) {
        const templateLeft = originX + template.x * cell;
        const templateTop = originY + template.y * cell;
        const templateRight = templateLeft + template.width * cell;
        const templateBottom = templateTop + template.height * cell;
        const templateVisibleLeft = Math.max(0, Math.floor(-templateLeft / cell));
        const templateVisibleTop = Math.max(0, Math.floor(-templateTop / cell));
        const templateVisibleRight = Math.min(template.width, Math.ceil((rect.width - templateLeft) / cell));
        const templateVisibleBottom = Math.min(template.height, Math.ceil((rect.height - templateTop) / cell));
        const templateVisibleWidth = templateVisibleRight - templateVisibleLeft;
        const templateVisibleHeight = templateVisibleBottom - templateVisibleTop;
        const templateVisible = templateSpaceHeldRef.current
          ? templateHoldToShowRef.current
          : templateHoldToShowRef.current
            ? templatePointerRevealRef.current
            : templateWatchPointerRef.current === null;
        if (templateVisibleWidth > 0 && templateVisibleHeight > 0 && templateVisible) {
          context.save();
          context.globalAlpha = Math.max(0, Math.min(1, templateOpacity));
          context.imageSmoothingEnabled = false;
          context.drawImage(
            template.canvas,
            templateVisibleLeft,
            templateVisibleTop,
            templateVisibleWidth,
            templateVisibleHeight,
            templateLeft + templateVisibleLeft * cell,
            templateTop + templateVisibleTop * cell,
            templateVisibleWidth * cell,
            templateVisibleHeight * cell,
          );
          context.restore();
        }

        const drawTemplateHandle = (centerX: number, centerY: number, kind: 'move' | 'resize' | 'watch') => {
          context.save();
          context.fillStyle = '#fff';
          context.strokeStyle = '#000';
          context.lineWidth = 2;
          context.beginPath();
          context.roundRect(centerX - 15, centerY - 15, 30, 30, 6);
          context.fill();
          context.stroke();
          const icon = kind === 'move' ? templateMoveIconRef.current : kind === 'resize' ? templateResizeIconRef.current : templateWatchIconRef.current;
          if (icon?.complete && icon.naturalWidth > 0) {
            const iconWidth = kind === 'watch' ? 22 : kind === 'move' ? 17 : 16;
            const iconHeight = kind === 'watch' ? 15 : iconWidth;
            if (kind === 'watch') context.filter = 'brightness(0)';
            context.drawImage(icon, centerX - iconWidth / 2, centerY - iconHeight / 2, iconWidth, iconHeight);
          }
          context.restore();
        };
        drawTemplateHandle(templateLeft + 18, templateTop + 18, 'move');
        drawTemplateHandle(templateRight - 18, templateTop + 18, 'watch');
        drawTemplateHandle(templateRight - 18, templateBottom - 18, 'resize');
      }
      const selected = selectedRef.current;
      if (selected) {
        context.save();
        const selectedLeft = originX + selected.x * cell;
        const selectedTop = originY + selected.y * cell;
        const selectedRight = selectedLeft + cell;
        const selectedBottom = selectedTop + cell;
        const selectedCenterX = selectedLeft + cell / 2;
        const selectedCenterY = selectedTop + cell / 2;
        const rayLength = Math.max(22, Math.min(72, cell * 3.5));
        context.lineWidth = Math.max(1, Math.min(3, cell * 0.14));
        context.lineCap = 'butt';
        context.globalCompositeOperation = 'difference';
        const drawRay = (startX: number, startY: number, endX: number, endY: number, fadeAtStart: boolean) => {
          const gradient = context.createLinearGradient(startX, startY, endX, endY);
          gradient.addColorStop(0, fadeAtStart ? 'rgba(255,255,255,0)' : 'rgba(255,255,255,.92)');
          gradient.addColorStop(1, fadeAtStart ? 'rgba(255,255,255,.92)' : 'rgba(255,255,255,0)');
          context.strokeStyle = gradient;
          context.beginPath();
          context.moveTo(startX, startY);
          context.lineTo(endX, endY);
          context.stroke();
        };
        drawRay(selectedCenterX, Math.max(originY, selectedTop - rayLength), selectedCenterX, selectedTop, true);
        drawRay(selectedCenterX, selectedBottom, selectedCenterX, Math.min(originY + boardHeight, selectedBottom + rayLength), false);
        drawRay(Math.max(originX, selectedLeft - rayLength), selectedCenterY, selectedLeft, selectedCenterY, true);
        drawRay(selectedRight, selectedCenterY, Math.min(originX + boardWidth, selectedRight + rayLength), selectedCenterY, false);
        context.globalCompositeOperation = 'source-over';
        context.strokeStyle = '#000000';
        context.lineWidth = Math.max(2, Math.min(4, cell * 0.18));
        context.strokeRect(selectedLeft + 1, selectedTop + 1, Math.max(0, cell - 2), Math.max(0, cell - 2));
        context.restore();
      }
    };

    drawRef.current = draw;
    draw();
    const observer = new ResizeObserver(draw);
    observer.observe(canvas);
    return () => observer.disconnect();
  }, [zoom, revision, templateOpacity]);

  function getCell(event: React.PointerEvent<HTMLCanvasElement>) {
    const rect = event.currentTarget.getBoundingClientRect();
    const { width, height } = boardSizeRef.current;
    const cell = renderedZoomRef.current;
    const originX = rect.width / 2 - (width * cell) / 2 + panRef.current.x;
    const originY = rect.height / 2 - (height * cell) / 2 + panRef.current.y;
    const x = Math.floor((event.clientX - rect.left - originX) / cell);
    const y = Math.floor((event.clientY - rect.top - originY) / cell);
    return { x, y };
  }

  function sample(event: React.PointerEvent<HTMLCanvasElement>) {
    const { x, y } = getCell(event);
    const { width, height } = boardSizeRef.current;
    if (x < 0 || y < 0 || x >= width || y >= height) return;
    onPickColor(normalizeBoardColor(pixelsRef.current.get(`${x}:${y}`)?.color ?? '#ffffff'));
  }

  function scheduleViewRender() {
    if (viewFrameRef.current !== null) return;
    viewFrameRef.current = window.requestAnimationFrame(() => {
      viewFrameRef.current = null;
      drawRef.current();
    });
  }

  function getTemplateHandles(canvas: HTMLCanvasElement) {
    const template = templateRef.current;
    if (!template) return null;
    const rect = canvas.getBoundingClientRect();
    const cell = renderedZoomRef.current;
    const { width, height } = boardSizeRef.current;
    const originX = rect.width / 2 - (width * cell) / 2 + panRef.current.x;
    const originY = rect.height / 2 - (height * cell) / 2 + panRef.current.y;
    const left = originX + template.x * cell;
    const top = originY + template.y * cell;
    const right = left + template.width * cell;
    const bottom = top + template.height * cell;
    const inset = 18;
    return {
      move: { x: left + inset, y: top + inset },
      watch: { x: right - inset, y: top + inset },
      resize: { x: right - inset, y: bottom - inset },
    };
  }

  function handlePointerDown(event: React.PointerEvent<HTMLCanvasElement>) {
    if (!boardReadyRef.current) return;
    if (event.button !== 0) return;
    event.currentTarget.setPointerCapture(event.pointerId);
    if (!eyedropper && templateRef.current) {
      const handles = getTemplateHandles(event.currentTarget);
      const rect = event.currentTarget.getBoundingClientRect();
      const localX = event.clientX - rect.left;
      const localY = event.clientY - rect.top;
      const hitRadius = 22;
      const watching = handles && Math.hypot(localX - handles.watch.x, localY - handles.watch.y) <= hitRadius;
      if (watching) {
        templateWatchPointerRef.current = event.pointerId;
        templatePointerRevealRef.current = templateHoldToShowRef.current;
        dragRef.current = null;
        scheduleViewRender();
        return;
      }
      const mode = handles && Math.hypot(localX - handles.move.x, localY - handles.move.y) <= hitRadius
        ? 'move'
        : handles && Math.hypot(localX - handles.resize.x, localY - handles.resize.y) <= hitRadius
          ? 'resize'
          : null;
      if (mode) {
        const template = templateRef.current;
        templateGestureRef.current = {
          mode,
          pointerId: event.pointerId,
          startClientX: event.clientX,
          startClientY: event.clientY,
          startX: template.x,
          startY: template.y,
          startWidth: template.width,
          startHeight: template.height,
        };
        dragRef.current = null;
        return;
      }
    }
    pointersRef.current.set(event.pointerId, { x: event.clientX, y: event.clientY });
    if (eyedropper) {
      dragRef.current = null;
      sample(event);
      return;
    }
    if (pointersRef.current.size === 2) {
      if (wheelAnimationRef.current !== null) {
        window.cancelAnimationFrame(wheelAnimationRef.current);
        wheelAnimationRef.current = null;
      }
      if (longPressTimerRef.current !== null) window.clearTimeout(longPressTimerRef.current);
      longPressTimerRef.current = null;
      dragRef.current = null;
      const [first, second] = [...pointersRef.current.values()];
      const center = { x: (first.x + second.x) / 2, y: (first.y + second.y) / 2 };
      const rect = event.currentTarget.getBoundingClientRect();
      const localCenter = { x: center.x - rect.left, y: center.y - rect.top };
      const { width, height } = boardSizeRef.current;
      const currentZoom = renderedZoomRef.current;
      const originX = rect.width / 2 - (width * currentZoom) / 2 + panRef.current.x;
      const originY = rect.height / 2 - (height * currentZoom) / 2 + panRef.current.y;
      const distance = Math.hypot(second.x - first.x, second.y - first.y);
      pinchRef.current = {
        distance,
        smoothedDistance: distance,
        zoom: currentZoom,
        center: localCenter,
        boardPoint: { x: (localCenter.x - originX) / currentZoom, y: (localCenter.y - originY) / currentZoom },
      };
      targetZoomRef.current = currentZoom;
      return;
    }
    const cell = getCell(event);
    dragRef.current = { x: event.clientX, y: event.clientY, cellX: cell.x, cellY: cell.y, moved: false, longPressed: false, pointerId: event.pointerId };
    longPressTimerRef.current = window.setTimeout(() => {
      const drag = dragRef.current;
      if (!drag || drag.moved) return;
      const pixel = pixelsRef.current.get(`${drag.cellX}:${drag.cellY}`);
      if (!pixel) return;
      drag.longPressed = true;
      onInspectPixel(pixel);
      navigator.vibrate?.(25);
    }, 520);
  }

  function handlePointerMove(event: React.PointerEvent<HTMLCanvasElement>) {
    if (templateWatchPointerRef.current === event.pointerId) return;
    const templateGesture = templateGestureRef.current;
    if (templateGesture?.pointerId === event.pointerId) {
      const template = templateRef.current;
      if (!template) return;
      const cell = Math.max(MIN_ZOOM, renderedZoomRef.current);
      const board = boardSizeRef.current;
      if (templateGesture.mode === 'move') {
        const nextX = Math.max(0, Math.min(board.width - template.width, Math.round(templateGesture.startX + (event.clientX - templateGesture.startClientX) / cell)));
        const nextY = Math.max(0, Math.min(board.height - template.height, Math.round(templateGesture.startY + (event.clientY - templateGesture.startClientY) / cell)));
        if (nextX !== template.x || nextY !== template.y) {
          template.x = nextX;
          template.y = nextY;
          scheduleViewRender();
        }
      } else {
        const aspect = templateGesture.startWidth / Math.max(1, templateGesture.startHeight);
        const horizontalDelta = (event.clientX - templateGesture.startClientX) / cell;
        const verticalDelta = (event.clientY - templateGesture.startClientY) / cell;
        const desiredWidth = templateGesture.startWidth + (horizontalDelta + verticalDelta * aspect) / 2;
        const maxWidth = Math.max(1, Math.min(board.width - templateGesture.startX, (board.height - templateGesture.startY) * aspect));
        const nextWidth = Math.max(1, Math.min(maxWidth, Math.round(desiredWidth)));
        const nextHeight = Math.max(1, Math.round(nextWidth / aspect));
        if (nextWidth !== template.width || nextHeight !== template.height) {
          template.x = templateGesture.startX;
          template.y = templateGesture.startY;
          template.width = nextWidth;
          template.height = nextHeight;
          scheduleViewRender();
        }
      }
      return;
    }
    const pointer = pointersRef.current.get(event.pointerId);
    if (pointer) { pointer.x = event.clientX; pointer.y = event.clientY; }
    if (eyedropper) {
      if (event.buttons) sample(event);
      return;
    }
    if (pinchRef.current && pointersRef.current.size >= 2) {
      // Pointer events can arrive much faster than the screen can repaint.
      // Keep only the latest positions and update once per animation frame.
      if (pinchFrameRef.current === null) {
        pinchFrameRef.current = window.requestAnimationFrame(() => {
          pinchFrameRef.current = null;
          const activePinch = pinchRef.current;
          if (!activePinch || pointersRef.current.size < 2 || !canvasRef.current) return;
          const [first, second] = [...pointersRef.current.values()];
          const rawDistance = Math.max(1, Math.hypot(second.x - first.x, second.y - first.y));
          const rect = canvasRef.current.getBoundingClientRect();
          const rawCenter = { x: (first.x + second.x) / 2 - rect.left, y: (first.y + second.y) / 2 - rect.top };
          activePinch.smoothedDistance += (rawDistance - activePinch.smoothedDistance) * 0.32;
          activePinch.center.x += (rawCenter.x - activePinch.center.x) * 0.28;
          activePinch.center.y += (rawCenter.y - activePinch.center.y) * 0.28;
          const distanceRatio = activePinch.smoothedDistance / Math.max(1, activePinch.distance);
          const desiredZoom = Math.min(MAX_ZOOM, Math.max(MIN_ZOOM, activePinch.zoom * Math.pow(distanceRatio, ZOOM_SPEED_MULTIPLIER)));
          const currentZoom = renderedZoomRef.current;
          const difference = desiredZoom - currentZoom;
          const unrestrictedZoom = currentZoom + difference * (0.28 * ZOOM_SPEED_MULTIPLIER);
          const frameFactor = 1 + 0.06 * ZOOM_SPEED_MULTIPLIER;
          const nextZoom = Math.min(currentZoom * frameFactor, Math.max(currentZoom / frameFactor, unrestrictedZoom));
          const nextOriginX = activePinch.center.x - activePinch.boardPoint.x * nextZoom;
          const nextOriginY = activePinch.center.y - activePinch.boardPoint.y * nextZoom;
          const { width, height } = boardSizeRef.current;
          panRef.current.x = nextOriginX - (rect.width / 2 - (width * nextZoom) / 2);
          panRef.current.y = nextOriginY - (rect.height / 2 - (height * nextZoom) / 2);
          renderedZoomRef.current = nextZoom;
          targetZoomRef.current = nextZoom;
          skipZoomReanchorRef.current = true;
          scheduleViewRender();
        });
      }
      return;
    }
    const drag = dragRef.current;
    if (!drag) return;
    const dx = event.clientX - drag.x;
    const dy = event.clientY - drag.y;
    if (Math.abs(dx) + Math.abs(dy) > 4) {
      drag.moved = true;
      if (longPressTimerRef.current !== null) window.clearTimeout(longPressTimerRef.current);
    }
    // A phone often emits a few small move events while the user is tapping.
    // Do not pan until the drag threshold is crossed, otherwise every tap
    // slowly shifts the board.
    if (!drag.moved) return;
    panRef.current.x += dx; panRef.current.y += dy;
    drag.x = event.clientX; drag.y = event.clientY;
    scheduleViewRender();
  }

  function handlePointerUp(event: React.PointerEvent<HTMLCanvasElement>) {
    if (templateWatchPointerRef.current === event.pointerId) {
      templateWatchPointerRef.current = null;
      templatePointerRevealRef.current = false;
      if (event.type === 'pointerup') {
        const previousTap = lastTemplateWatchTapRef.current;
        if (previousTap > 0 && event.timeStamp - previousTap <= 450) {
          templateHoldToShowRef.current = !templateHoldToShowRef.current;
          lastTemplateWatchTapRef.current = 0;
          navigator.vibrate?.(25);
        } else {
          lastTemplateWatchTapRef.current = event.timeStamp;
        }
      }
      scheduleViewRender();
      return;
    }
    const templateGesture = templateGestureRef.current;
    if (templateGesture?.pointerId === event.pointerId) {
      templateGestureRef.current = null;
      const template = templateRef.current;
      if (template) {
        if (templateGesture.mode === 'resize') template.canvas = renderTemplate(template.image, template.width, template.height);
        onTemplatePlacementChange({ x: template.x, y: template.y, width: template.width, height: template.height });
        scheduleViewRender();
      }
      return;
    }
    pointersRef.current.delete(event.pointerId);
    if (pinchFrameRef.current !== null) {
      window.cancelAnimationFrame(pinchFrameRef.current);
      pinchFrameRef.current = null;
    }
    if (longPressTimerRef.current !== null) window.clearTimeout(longPressTimerRef.current);
    longPressTimerRef.current = null;
    if (pinchRef.current) {
      pinchRef.current = null;
      dragRef.current = null;
      onZoom(renderedZoomRef.current);
      return;
    }
    const drag = dragRef.current;
    dragRef.current = null;
    if (eyedropper) {
      onEyedropperEnd();
      return;
    }
    if (!drag || drag.moved || drag.longPressed) return;
    const { x, y } = getCell(event);
    const { width, height } = boardSizeRef.current;
    if (x < 0 || y < 0 || x >= width || y >= height) return;
    selectedRef.current = { x, y };
    setRevision((value) => value + 1);
    onSelectPixel({ x, y });
    onInspectPixel(pixelsRef.current.get(`${x}:${y}`) ?? null);
  }

  function zoomWithWheel(event: WheelEvent) {
    event.preventDefault();
    const canvas = canvasRef.current;
    if (!canvas) return;
    const rect = canvas.getBoundingClientRect();
    const anchorX = event.clientX - rect.left;
    const anchorY = event.clientY - rect.top;
    const { width, height } = boardSizeRef.current;
    const currentZoom = renderedZoomRef.current;
    const currentOriginX = rect.width / 2 - (width * currentZoom) / 2 + panRef.current.x;
    const currentOriginY = rect.height / 2 - (height * currentZoom) / 2 + panRef.current.y;
    wheelAnchorRef.current = {
      x: anchorX,
      y: anchorY,
      boardX: (anchorX - currentOriginX) / currentZoom,
      boardY: (anchorY - currentOriginY) / currentZoom,
    };
    const normalizedDelta = Math.max(-120, Math.min(120, event.deltaY));
    const factor = Math.exp(-normalizedDelta * 0.0015 * ZOOM_SPEED_MULTIPLIER);
    targetZoomRef.current = Math.min(MAX_ZOOM, Math.max(MIN_ZOOM, targetZoomRef.current * factor));
    if (wheelAnimationRef.current !== null) return;

    const animateZoom = () => {
      const canvas = canvasRef.current;
      const anchor = wheelAnchorRef.current;
      if (!canvas || !anchor) { wheelAnimationRef.current = null; return; }
      const current = renderedZoomRef.current;
      const target = targetZoomRef.current;
      const difference = target - current;
      const threshold = Math.max(0.002, target * 0.001);
      const next = Math.abs(difference) < threshold ? target : current + difference * (0.18 * ZOOM_SPEED_MULTIPLIER);
      const canvasRect = canvas.getBoundingClientRect();
      const boardSize = boardSizeRef.current;
      const nextOriginX = anchor.x - anchor.boardX * next;
      const nextOriginY = anchor.y - anchor.boardY * next;
      panRef.current.x = nextOriginX - (canvasRect.width / 2 - (boardSize.width * next) / 2);
      panRef.current.y = nextOriginY - (canvasRect.height / 2 - (boardSize.height * next) / 2);
      renderedZoomRef.current = next;
      skipZoomReanchorRef.current = true;
      scheduleViewRender();
      if (next === target) {
        wheelAnimationRef.current = null;
        onZoom(next);
        return;
      }
      wheelAnimationRef.current = window.requestAnimationFrame(animateZoom);
    };
    wheelAnimationRef.current = window.requestAnimationFrame(animateZoom);
  }

  function handleWheel(event: React.WheelEvent<HTMLCanvasElement>) {
    zoomWithWheel(event.nativeEvent);
  }

  useEffect(() => {
    const handleCtrlWheel = (event: WheelEvent) => {
      if (!event.ctrlKey) return;
      event.preventDefault();
      event.stopPropagation();
      zoomWithWheel(event);
    };
    window.addEventListener('wheel', handleCtrlWheel, { capture: true, passive: false });
    return () => window.removeEventListener('wheel', handleCtrlWheel, { capture: true });
  }, [onZoom]);

  useEffect(() => {
    const handleTemplateSpace = (event: KeyboardEvent) => {
      const isSpace = event.code === 'Space' || event.key === ' ' || event.key === 'Spacebar';
      if (!isSpace || event.altKey || event.ctrlKey || event.metaKey) return;
      if (event.type === 'keyup') {
        if (!templateSpaceHeldRef.current) return;
        event.preventDefault();
        templateSpaceHeldRef.current = false;
        setRevision((value) => value + 1);
        return;
      }
      const target = event.target;
      if (target instanceof HTMLElement && target.closest('input:not([type="button"]):not([type="submit"]):not([type="reset"]), textarea, select, [contenteditable="true"]')) return;
      if (!templateRef.current) return;
      event.preventDefault();
      if (event.repeat || templateSpaceHeldRef.current) return;
      templateSpaceHeldRef.current = true;
      setRevision((value) => value + 1);
    };
    const releaseTemplateSpace = () => {
      if (!templateSpaceHeldRef.current) return;
      templateSpaceHeldRef.current = false;
      setRevision((value) => value + 1);
    };
    window.addEventListener('keydown', handleTemplateSpace, true);
    window.addEventListener('keyup', handleTemplateSpace, true);
    window.addEventListener('blur', releaseTemplateSpace);
    return () => {
      window.removeEventListener('keydown', handleTemplateSpace, true);
      window.removeEventListener('keyup', handleTemplateSpace, true);
      window.removeEventListener('blur', releaseTemplateSpace);
    };
  }, []);

  useEffect(() => {
    const handleArrowSelection = (event: KeyboardEvent) => {
      const movement: Record<string, { x: number; y: number }> = {
        ArrowUp: { x: 0, y: -1 },
        ArrowDown: { x: 0, y: 1 },
        ArrowLeft: { x: -1, y: 0 },
        ArrowRight: { x: 1, y: 0 },
      };
      const delta = movement[event.key];
      if (!delta || event.altKey || event.ctrlKey || event.metaKey) return;
      const target = event.target;
      if (target instanceof HTMLElement && target.closest('input, textarea, select, [contenteditable="true"]')) return;
      const selected = selectedRef.current;
      if (!selected) return;
      event.preventDefault();
      const { width, height } = boardSizeRef.current;
      const x = Math.max(0, Math.min(width - 1, selected.x + delta.x));
      const y = Math.max(0, Math.min(height - 1, selected.y + delta.y));
      if (x === selected.x && y === selected.y) return;
      selectedRef.current = { x, y };
      onSelectPixel({ x, y });
      onInspectPixel(pixelsRef.current.get(`${x}:${y}`) ?? null);
      scheduleViewRender();
    };
    window.addEventListener('keydown', handleArrowSelection);
    return () => window.removeEventListener('keydown', handleArrowSelection);
  }, [onInspectPixel, onSelectPixel]);

  return (
    <div className="board-wrap">
      <canvas ref={canvasRef} className={`pixel-board${eyedropper ? ' eyedropper-mode' : ''}`} onPointerDown={handlePointerDown} onPointerMove={handlePointerMove} onPointerUp={handlePointerUp} onPointerCancel={handlePointerUp} onContextMenu={(event) => event.preventDefault()} onWheel={handleWheel} />
    </div>
  );
}

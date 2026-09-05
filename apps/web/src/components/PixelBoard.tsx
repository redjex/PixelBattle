import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react';
import { usePixelSocket } from '../hooks/usePixelSocket';
import type { Pixel } from '../types/pixel';
import { loadBoardSnapshot } from '../boardSnapshot';

const DEFAULT_BOARD_SIZE = 150;
const MIN_ZOOM = 0.5;
const MAX_ZOOM = 100;
const TEMPLATE_COLORS = [
  '#FF8080', '#FFCA73', '#FBFFA5', '#7CFF80', '#7EFFF2', '#84D0FF', '#8290FF', '#CD81FF', '#FF80D0', '#FDFDFD',
  '#FF0000', '#FF9D00', '#F2FF00', '#00FF07', '#00FFE6', '#009DFF', '#001EFF', '#9900FF', '#FF00A1', '#8A8A8A',
  '#870000', '#8D4E00', '#B6A700', '#009904', '#009687', '#00568C', '#001194', '#53008A', '#8E005A', '#000000',
].map((color) => ({ color, red: Number.parseInt(color.slice(1, 3), 16), green: Number.parseInt(color.slice(3, 5), 16), blue: Number.parseInt(color.slice(5, 7), 16) }));
const TEMPLATE_NEUTRAL_COLORS = TEMPLATE_COLORS.filter((color) => Math.max(color.red, color.green, color.blue) - Math.min(color.red, color.green, color.blue) <= 8);

const normalizeBoardColor = (color: string) => color.toUpperCase() === '#F8F9FA' ? '#FFFFFF' : color;

type TemplateState = { image: HTMLImageElement; canvas: HTMLCanvasElement; x: number; y: number; width: number; height: number };
type TemplateGesture = { mode: 'move' | 'resize'; pointerId: number; startClientX: number; startClientY: number; startX: number; startY: number; startWidth: number; startHeight: number };
type Props = { color: string; zoom: number; onZoom: (zoom: number) => void; eyedropper: boolean; onPickColor: (color: string) => void; onEyedropperEnd: () => void; paintNonce: number; useIce: boolean; onSelectPixel: (pixel: { x: number; y: number } | null) => void; onInspectPixel: (pixel: Pixel | null) => void; cooldownUntil: number; onPlacementAccepted: () => void; templateImageUrl: string | null };

function renderTemplate(image: HTMLImageElement, width: number, height: number) {
  const canvas = document.createElement('canvas');
  canvas.width = width;
  canvas.height = height;
  const context = canvas.getContext('2d', { willReadFrequently: true });
  if (!context) return canvas;
  context.imageSmoothingEnabled = true;
  context.imageSmoothingQuality = 'high';
  context.drawImage(image, 0, 0, width, height);
  const data = context.getImageData(0, 0, width, height);
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

export function PixelBoard({ color, zoom, onZoom, eyedropper, onPickColor, onEyedropperEnd, paintNonce, useIce, onSelectPixel, onInspectPixel, cooldownUntil, onPlacementAccepted, templateImageUrl }: Props) {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const checkerPatternRef = useRef<CanvasPattern | null>(null);
  const boardLayerRef = useRef<HTMLCanvasElement | null>(null);
  const boardLayerDirtyRef = useRef(true);
  const pixelsRef = useRef(new Map<string, Pixel>());
  const boardReadyRef = useRef(false);
  const templateRef = useRef<TemplateState | null>(null);
  const templateGestureRef = useRef<TemplateGesture | null>(null);
  const templateMoveIconRef = useRef<HTMLImageElement | null>(null);
  const templateResizeIconRef = useRef<HTMLImageElement | null>(null);
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
  const wheelAnchorRef = useRef<{ x: number; y: number; boardX: number; boardY: number } | null>(null);
  const skipZoomReanchorRef = useRef(false);
  const longPressTimerRef = useRef<number | null>(null);
  const selectedRef = useRef<{ x: number; y: number } | null>(null);
  const [revision, setRevision] = useState(0);
  const [boardReloadNonce, setBoardReloadNonce] = useState(0);
  const [boardDimensions, setBoardDimensions] = useState<{ width: number; height: number } | null>(null);

  const acceptPixel = useCallback((pixel: Pixel) => {
    pixelsRef.current.set(`${pixel.x}:${pixel.y}`, pixel);
    boardLayerDirtyRef.current = true;
    const selected = selectedRef.current;
    if (selected?.x === pixel.x && selected.y === pixel.y) onInspectPixel(pixel);
    setRevision((value) => value + 1);
  }, [onInspectPixel]);
  const reloadBoard = useCallback(() => setBoardReloadNonce((value) => value + 1), []);
  const { place } = usePixelSocket(acceptPixel, reloadBoard);

  useEffect(() => {
    const moveIcon = new Image();
    const resizeIcon = new Image();
    const refresh = () => setRevision((value) => value + 1);
    moveIcon.onload = refresh;
    resizeIcon.onload = refresh;
    moveIcon.src = '/assets/move.svg';
    resizeIcon.src = '/assets/upscale.svg';
    templateMoveIconRef.current = moveIcon;
    templateResizeIconRef.current = resizeIcon;
    return () => {
      moveIcon.onload = null;
      resizeIcon.onload = null;
      templateMoveIconRef.current = null;
      templateResizeIconRef.current = null;
    };
  }, []);

  useLayoutEffect(() => {
    const previousZoom = previousZoomRef.current;
    previousZoomRef.current = zoom;
    renderedZoomRef.current = zoom;
    if (wheelAnimationRef.current === null) targetZoomRef.current = zoom;
    if (previousZoom === zoom || skipZoomReanchorRef.current) {
      skipZoomReanchorRef.current = false;
      return;
    }
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
    setRevision((value) => value + 1);
    if (!templateImageUrl || !boardDimensions) return;
    let disposed = false;
    const image = new Image();
    image.onload = () => {
      if (disposed || !image.naturalWidth || !image.naturalHeight || image.naturalWidth * image.naturalHeight > 100_000_000) return;
      const scale = Math.min(1, boardDimensions.width / image.naturalWidth, boardDimensions.height / image.naturalHeight);
      const width = Math.max(1, Math.floor(image.naturalWidth * scale));
      const height = Math.max(1, Math.floor(image.naturalHeight * scale));
      const canvas = renderTemplate(image, width, height);
      templateRef.current = {
        image,
        canvas,
        width,
        height,
        x: Math.floor((boardDimensions.width - width) / 2),
        y: Math.floor((boardDimensions.height - height) / 2),
      };
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
        onPlacementAccepted();
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
      const ratio = window.devicePixelRatio || 1;
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

      const cell = zoom;
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
      context.imageSmoothingEnabled = false;
      context.drawImage(boardLayer, originX, originY, boardWidth, boardHeight);
      context.save();
      context.strokeStyle = '#111111';
      context.lineWidth = 3;
      context.lineCap = 'round';
      context.lineJoin = 'miter';
      const borderX = originX + 1.5;
      const borderY = originY + 1.5;
      const borderWidth = Math.max(0, boardWidth - 3);
      const borderHeight = Math.max(0, boardHeight - 3);
      const drawPillEdge = (startX: number, startY: number, endX: number, endY: number) => {
        const length = Math.hypot(endX - startX, endY - startY);
        if (length <= 0) return;
        const unitX = (endX - startX) / length;
        const unitY = (endY - startY) / length;
        const dashLength = 8;
        const gapLength = 7;
        for (let offset = gapLength / 2; offset < length - gapLength / 2; offset += dashLength + gapLength) {
          const dashEnd = Math.min(offset + dashLength, length - gapLength / 2);
          context.beginPath();
          context.moveTo(startX + unitX * offset, startY + unitY * offset);
          context.lineTo(startX + unitX * dashEnd, startY + unitY * dashEnd);
          context.stroke();
        }
      };
      drawPillEdge(borderX, borderY, borderX + borderWidth, borderY);
      drawPillEdge(borderX + borderWidth, borderY, borderX + borderWidth, borderY + borderHeight);
      drawPillEdge(borderX + borderWidth, borderY + borderHeight, borderX, borderY + borderHeight);
      drawPillEdge(borderX, borderY + borderHeight, borderX, borderY);
      context.restore();
      const template = templateRef.current;
      if (template) {
        const templateLeft = originX + template.x * cell;
        const templateTop = originY + template.y * cell;
        const templateRight = templateLeft + template.width * cell;
        const templateBottom = templateTop + template.height * cell;
        context.save();
        context.globalAlpha = 0.46;
        context.imageSmoothingEnabled = false;
        context.drawImage(template.canvas, templateLeft, templateTop, template.width * cell, template.height * cell);
        context.restore();

        const drawTemplateHandle = (centerX: number, centerY: number, kind: 'move' | 'resize') => {
          context.save();
          context.fillStyle = '#fff';
          context.strokeStyle = '#000';
          context.lineWidth = 2;
          context.beginPath();
          context.roundRect(centerX - 15, centerY - 15, 30, 30, 6);
          context.fill();
          context.stroke();
          const icon = kind === 'move' ? templateMoveIconRef.current : templateResizeIconRef.current;
          if (icon?.complete && icon.naturalWidth > 0) {
            const iconSize = kind === 'move' ? 17 : 16;
            context.drawImage(icon, centerX - iconSize / 2, centerY - iconSize / 2, iconSize, iconSize);
          }
          context.restore();
        };
        drawTemplateHandle(templateLeft + 18, templateTop + 18, 'move');
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

    draw();
    const observer = new ResizeObserver(draw);
    observer.observe(canvas);
    return () => observer.disconnect();
  }, [zoom, revision]);

  function getCell(event: React.PointerEvent<HTMLCanvasElement>) {
    const rect = event.currentTarget.getBoundingClientRect();
    const { width, height } = boardSizeRef.current;
    const originX = rect.width / 2 - (width * zoom) / 2 + panRef.current.x;
    const originY = rect.height / 2 - (height * zoom) / 2 + panRef.current.y;
    const x = Math.floor((event.clientX - rect.left - originX) / zoom);
    const y = Math.floor((event.clientY - rect.top - originY) / zoom);
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
      setRevision((value) => value + 1);
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
      resize: { x: right - inset, y: bottom - inset },
    };
  }

  function handlePointerDown(event: React.PointerEvent<HTMLCanvasElement>) {
    if (!boardReadyRef.current) return;
    event.currentTarget.setPointerCapture(event.pointerId);
    if (!eyedropper && templateRef.current) {
      const handles = getTemplateHandles(event.currentTarget);
      const rect = event.currentTarget.getBoundingClientRect();
      const localX = event.clientX - rect.left;
      const localY = event.clientY - rect.top;
      const hitRadius = 22;
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
          template.canvas = renderTemplate(template.image, nextWidth, nextHeight);
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
          const desiredZoom = Math.min(MAX_ZOOM, Math.max(MIN_ZOOM, activePinch.zoom * activePinch.smoothedDistance / Math.max(1, activePinch.distance)));
          const currentZoom = renderedZoomRef.current;
          const difference = desiredZoom - currentZoom;
          const unrestrictedZoom = currentZoom + difference * 0.28;
          const nextZoom = Math.min(currentZoom * 1.06, Math.max(currentZoom / 1.06, unrestrictedZoom));
          const nextOriginX = activePinch.center.x - activePinch.boardPoint.x * nextZoom;
          const nextOriginY = activePinch.center.y - activePinch.boardPoint.y * nextZoom;
          const { width, height } = boardSizeRef.current;
          panRef.current.x = nextOriginX - (rect.width / 2 - (width * nextZoom) / 2);
          panRef.current.y = nextOriginY - (rect.height / 2 - (height * nextZoom) / 2);
          renderedZoomRef.current = nextZoom;
          targetZoomRef.current = nextZoom;
          skipZoomReanchorRef.current = true;
          onZoom(nextZoom);
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
    if (templateGestureRef.current?.pointerId === event.pointerId) {
      templateGestureRef.current = null;
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
    const factor = Math.exp(-normalizedDelta * 0.0015);
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
      const next = Math.abs(difference) < threshold ? target : current + difference * 0.18;
      const canvasRect = canvas.getBoundingClientRect();
      const boardSize = boardSizeRef.current;
      const nextOriginX = anchor.x - anchor.boardX * next;
      const nextOriginY = anchor.y - anchor.boardY * next;
      panRef.current.x = nextOriginX - (canvasRect.width / 2 - (boardSize.width * next) / 2);
      panRef.current.y = nextOriginY - (canvasRect.height / 2 - (boardSize.height * next) / 2);
      renderedZoomRef.current = next;
      skipZoomReanchorRef.current = true;
      onZoom(next);
      if (next === target) {
        wheelAnimationRef.current = null;
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

  return (
    <div className="board-wrap">
      <canvas ref={canvasRef} className={`pixel-board${eyedropper ? ' eyedropper-mode' : ''}`} onPointerDown={handlePointerDown} onPointerMove={handlePointerMove} onPointerUp={handlePointerUp} onPointerCancel={handlePointerUp} onContextMenu={(event) => event.preventDefault()} onWheel={handleWheel} />
    </div>
  );
}

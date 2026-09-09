import { useCallback, useEffect, useRef, useState } from 'react';
import type { Pixel, PlacementMessage } from '../types/pixel';

export function usePixelSocket(onPixel: (pixel: Pixel) => void, onBoardReload: () => void) {
  const socketRef = useRef<WebSocket | null>(null);
  const reconnectRef = useRef<number | null>(null);
  const pendingRef = useRef(new Map<string, { resolve: (accepted: boolean) => void; timer: number }>());
  const [connected, setConnected] = useState(false);

  useEffect(() => {
    let disposed = false;
    const telegram = window.Telegram?.WebApp;
    if (!telegram?.initData) return;
    const initData = telegram.initData;
    const configuredUrl = import.meta.env.VITE_WS_URL;
    const url = configuredUrl ?? `${window.location.protocol === 'https:' ? 'wss:' : 'ws:'}//${window.location.host}/ws`;
    if (!/^wss?:\/\//.test(url) || (window.location.protocol === 'https:' && !url.startsWith('wss://'))) return;

    function connect() {
      if (disposed) return;
      const socket = new WebSocket(url);
      const openedAt = Date.now();
      socketRef.current = socket;
      socket.onopen = () => {
        socket.send(JSON.stringify({ type: 'authenticate', initData }));
        setConnected(true);
      };
      socket.onmessage = (event) => {
        try {
          const message = JSON.parse(event.data) as Pixel & { type?: string };
          if (message.type === 'error') {
            // Authentication failures are terminal for this captured initData.
            const code = String((message as { code?: unknown }).code ?? '').toLowerCase();
            if (code.includes('auth') || code.includes('telegram') || code.includes('expired') || code.includes('credential')) {
              disposed = true;
              socket.close();
            }
          } else if (message.type === 'pixel_placed') {
            onPixel(message);
            if (message.operationId) {
              const pending = pendingRef.current.get(message.operationId);
              if (pending) {
                window.clearTimeout(pending.timer);
                pendingRef.current.delete(message.operationId);
                pending.resolve(true);
              }
            }
          } else if (message.type === 'board_reload') {
            onBoardReload();
          }
        } catch { /* Ignore malformed server messages. */ }
      };
      socket.onclose = (event) => {
        if (event.code === 4401) disposed = true;
        if (Date.now() - openedAt >= 60000) reconnectAttemptRef.current = 0;
        setConnected(false);
        for (const pending of pendingRef.current.values()) {
          window.clearTimeout(pending.timer);
          pending.resolve(false);
        }
        pendingRef.current.clear();
        if (!disposed) {
          const attempt = Math.min(6, reconnectAttemptRef.current++);
          reconnectRef.current = window.setTimeout(connect, Math.min(30000, 1000 * 2 ** attempt));
        }
      };
      socket.onerror = () => socket.close();
    }

    const reconnectAttemptRef = { current: 0 };
    connect();
    return () => {
      disposed = true;
      if (reconnectRef.current) window.clearTimeout(reconnectRef.current);
      for (const pending of pendingRef.current.values()) {
        window.clearTimeout(pending.timer);
        pending.resolve(false);
      }
      pendingRef.current.clear();
      socketRef.current?.close();
    };
  }, [onPixel, onBoardReload]);

  const place = useCallback((message: PlacementMessage) => {
    const initData = window.Telegram?.WebApp?.initData;
    if (!initData) return Promise.resolve(null);
    const apiUrl = import.meta.env.VITE_API_URL ?? window.location.origin;
    return fetch(`${apiUrl}/api/boards/main/pixels`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'X-Telegram-Init-Data': initData },
      body: JSON.stringify(message),
      cache: 'no-store',
      keepalive: true,
    }).then(async (response) => {
      if (!response.ok) return null;
      const event = await response.json() as Pixel;
      onPixel(event);
      return event;
    }).catch(() => null);
  }, [onPixel]);

  return { connected, place };
}

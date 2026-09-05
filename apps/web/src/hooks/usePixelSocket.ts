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
    const url = import.meta.env.VITE_WS_URL ?? 'ws://localhost:8080/ws';

    function connect() {
      if (disposed) return;
      const socket = new WebSocket(url);
      socketRef.current = socket;
      socket.onopen = () => {
        socket.send(JSON.stringify({ type: 'authenticate', initData }));
        setConnected(true);
      };
      socket.onmessage = (event) => {
        try {
          const message = JSON.parse(event.data) as Pixel & { type?: string };
          if (message.type === 'pixel_placed') {
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
      socket.onclose = () => {
        setConnected(false);
        for (const pending of pendingRef.current.values()) {
          window.clearTimeout(pending.timer);
          pending.resolve(false);
        }
        pendingRef.current.clear();
        reconnectRef.current = window.setTimeout(connect, 1500);
      };
      socket.onerror = () => socket.close();
    }

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

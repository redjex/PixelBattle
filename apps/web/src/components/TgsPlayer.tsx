import { useEffect, useRef, useState } from 'react';
import type { AnimationItem } from 'lottie-web';
import lottie from 'lottie-web/build/player/lottie_light';

type Props = { src: string; className?: string; loop?: boolean };

export function TgsPlayer({ src, className, loop = true }: Props) {
  const hostRef = useRef<HTMLDivElement>(null);
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    let animation: AnimationItem | undefined;
    let cancelled = false;

    async function load() {
      try {
        const response = await fetch(src);
        if (!response.ok) throw new Error('Animation file is not available');
        let json: unknown;
        const sourcePath = new URL(src, window.location.href).pathname.toLowerCase();
        if (sourcePath.endsWith('.tgs')) {
          if (!response.body) throw new Error('TGS file is not available');
          const stream = response.body.pipeThrough(new DecompressionStream('gzip'));
          json = await new Response(stream).json();
        } else {
          json = await response.json();
        }
        if (cancelled || !hostRef.current) return;
        animation = lottie.loadAnimation({
          container: hostRef.current,
          renderer: 'svg',
          loop,
          autoplay: true,
          animationData: json,
        });
      } catch {
        if (!cancelled) setFailed(true);
      }
    }

    void load();
    return () => {
      cancelled = true;
      animation?.destroy();
    };
  }, [loop, src]);

  if (failed) {
    return null;
  }

  return <div ref={hostRef} className={className} aria-label="Загрузочная анимация" />;
}

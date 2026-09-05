/// <reference types="vite/client" />

interface Window { Telegram?: { WebApp?: import('./telegram').TelegramWebApp } }

declare module 'lottie-web/build/player/lottie_light' {
  import type { LottiePlayer } from 'lottie-web';
  const lottie: LottiePlayer;
  export default lottie;
}

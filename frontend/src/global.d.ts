import type { BackendBridge } from './lib/backend';
import type { DetailedHTMLProps, HTMLAttributes } from 'react';

declare module 'react' {
  namespace JSX {
    interface IntrinsicElements {
      webview: DetailedHTMLProps<HTMLAttributes<HTMLElement>, HTMLElement> & {
        src?: string;
        partition?: string;
        title?: string;
      };
    }
  }
}

declare global {
  interface Window {
    aStock?: BackendBridge;
  }
}

export {};

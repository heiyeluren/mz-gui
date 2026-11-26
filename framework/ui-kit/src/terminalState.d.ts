export const terminalState: {
  defaultTheme: {background:string;foreground:string;cursor:string};
  attach(terminal: unknown): void;
  setDefaultColors(terminal: unknown, colors?: {foreground?: string; background?: string; cursor?: string}): void;
  colorReply(terminal: unknown, code: number): string;
  capture(terminal: unknown, addon: unknown, pending?: string): unknown;
  restore(terminal: unknown, state: unknown, done: (error?: unknown) => void): void;
  validate(state: unknown): void;
  pendingTracker(): { feed(data: string): void; get(): string; reset(prefix?: string): void };
};

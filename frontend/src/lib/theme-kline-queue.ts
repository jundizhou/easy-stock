import { KLine } from './backend';

export type KLineEntry = { lines?: KLine[]; updatedAt?: number; day?: string; pending: boolean; error?: string };
type Flight = { controller: AbortController; selected: boolean };
export const marketDay = () => new Intl.DateTimeFormat('sv-SE', { timeZone: 'Asia/Shanghai' }).format(new Date());

// One queue serves both the chart and row metrics. Three background slots leave
// room for an interactive selection even while another stock is slow.
export class ThemeKLineQueue {
	readonly entries = new Map<string, KLineEntry>();
	private flights = new Map<string, Flight>();
	private wanted: string[] = [];
	private foreground = new Set<string>();
	private selected = '';
	private paused = false;
	constructor(private fetchLines: (symbol: string, signal: AbortSignal) => Promise<KLine[]>, private changed: () => void, private now = Date.now, private day = marketDay) {}

	setWanted(symbols: string[], selected: string, prefetch: string[] = []) {
		this.paused = false;
		this.selected = selected;
		this.foreground = new Set([selected, ...symbols].filter(Boolean));
		this.wanted = [...new Set([selected, ...symbols, ...prefetch].filter(Boolean))];
		for (const [symbol, flight] of this.flights) {
			if (!this.wanted.includes(symbol) || (flight.selected && symbol !== selected)) flight.controller.abort();
		}
		for (const [symbol, entry] of this.entries) {
			if (!this.wanted.includes(symbol) && !this.flights.has(symbol)) this.entries.set(symbol, { ...entry, pending: false });
		}
		for (const symbol of this.wanted) {
			const entry = this.entries.get(symbol);
			const expired = entry?.day !== this.day() || this.now() - (entry.updatedAt || 0) > 60_000;
			if (!this.flights.has(symbol) && (!entry || (expired && (!entry.error || entry.day !== this.day())))) {
				this.entries.set(symbol, { ...entry, lines: entry?.day === this.day() ? entry.lines : undefined, pending: true, error: undefined });
			}
		}
		// Bound the session cache while retaining currently used stocks.
		for (const symbol of this.entries.keys()) {
			if (this.entries.size <= 120) break;
			if (!this.wanted.includes(symbol) && !this.flights.has(symbol)) this.entries.delete(symbol);
		}
		this.changed(); this.pump();
	}

	refresh() {
		for (const symbol of this.wanted) {
			const entry = this.entries.get(symbol);
			if (entry && !this.flights.has(symbol)) this.entries.set(symbol, { ...entry, pending: true, error: undefined });
		}
		this.changed(); this.pump();
	}

	retryFailed() {
		for (const symbol of this.wanted) {
			const entry = this.entries.get(symbol);
			if (entry?.error) this.entries.set(symbol, { ...entry, pending: true, error: undefined });
		}
		this.changed(); this.pump();
	}

	pause() {
		this.paused = true;
		for (const flight of this.flights.values()) flight.controller.abort();
		for (const [symbol, entry] of this.entries) this.entries.set(symbol, { ...entry, pending: false });
	}

	private pump() {
		if (this.paused) return;
		while (this.flights.size < 4) {
			const foregroundPending = [...this.foreground].some(symbol => this.entries.get(symbol)?.pending || this.flights.has(symbol));
			const backgrounds = [...this.flights.values()].filter(f => !f.selected).length;
			const symbol = this.wanted.find(symbol => this.entries.get(symbol)?.pending && !this.flights.has(symbol)
				&& (symbol === this.selected || backgrounds < 3)
				&& (this.foreground.has(symbol) || !foregroundPending));
			if (!symbol) break;
			this.start(symbol);
		}
	}

	private start(symbol: string) {
		const controller = new AbortController();
		const flight = { controller, selected: symbol === this.selected };
		this.flights.set(symbol, flight);
		let timedOut = false;
		const timer = setTimeout(() => { timedOut = true; controller.abort(); }, 25_000);
		void this.fetchLines(symbol, controller.signal).then(lines => {
			if (controller.signal.aborted) return;
			if (!lines.length) throw new Error('暂无日 K 数据');
			this.entries.set(symbol, { lines: [...lines].sort((a, b) => a.time.localeCompare(b.time)), updatedAt: this.now(), day: this.day(), pending: false });
		}).catch(reason => {
			if (!controller.signal.aborted || timedOut) this.entries.set(symbol, { ...this.entries.get(symbol), day: this.day(), updatedAt: this.now(), pending: false, error: timedOut ? '日 K 加载超时' : reason instanceof Error ? reason.message : '日 K 暂不可用' });
		}).finally(() => {
			clearTimeout(timer);
			this.flights.delete(symbol);
			if (controller.signal.aborted && !timedOut && !this.paused && this.wanted.includes(symbol)) {
				this.entries.set(symbol, { ...this.entries.get(symbol), pending: true });
			}
			if (!this.paused) { this.changed(); this.pump(); }
		});
	}
}

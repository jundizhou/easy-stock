import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest';
import { createMonitorLiveBus, selectMonitorQuoteValue, type LiveStreamStatus } from './monitorLiveBus';

class FakeEventSource {
	static instances: FakeEventSource[] = [];
	onmessage: ((event: MessageEvent) => void) | null = null;
	onopen: (() => void) | null = null;
	onerror: (() => void) | null = null;
	close = vi.fn();
	constructor(public url: string) { FakeEventSource.instances.push(this); }
	emit(data: unknown) { this.onmessage?.({ data: typeof data === 'string' ? data : JSON.stringify(data) } as MessageEvent); }
}

const config = { backendUrl: 'http://127.0.0.1:20081', token: 'local-token' };
const frame = (price = 12.34, change = 1.25, ts = 100) => ({ ticks: [{ symbol: '600000.SH', price, change_percent: change, ts }] });

beforeEach(() => {
	FakeEventSource.instances = [];
	vi.stubGlobal('EventSource', FakeEventSource);
});
afterEach(() => { vi.useRealTimers(); vi.unstubAllGlobals(); });

describe('monitor live bus', () => {
	it('uses HTTP values during disconnect or expiry and returns to healthy live quotes', () => {
		const quote = { price: 10, changePercent: 1, ts: 100 };
		const open: LiveStreamStatus = { connection: 'open', stale: false, sessionOpen: true, source: '', warning: '' };
		expect(selectMonitorQuoteValue(quote, open, 'price', 20)).toBe(10);
		for (const status of [{ ...open, connection: 'reconnecting' as const }, { ...open, stale: true }]) {
			expect(selectMonitorQuoteValue(quote, status, 'price', 20)).toBe(20);
			expect(selectMonitorQuoteValue(quote, status, 'change', 0)).toBe(0);
			expect(selectMonitorQuoteValue(quote, status, 'price', null)).toBe(10);
			expect(selectMonitorQuoteValue(quote, status, 'price', 0)).toBe(10);
		}
		expect(selectMonitorQuoteValue({ ...quote, stale: true }, open, 'price', 20)).toBe(20);
		expect(selectMonitorQuoteValue(quote, open, 'price', 20)).toBe(10);
		expect(selectMonitorQuoteValue(undefined, open, 'change', 0)).toBe(0);
		expect(selectMonitorQuoteValue(undefined, open, 'price', NaN)).toBeUndefined();
	});
	it('shares live changes with summaries without notifying unrelated price cells', () => {
		const bus = createMonitorLiveBus();
		const off = bus.connect(config);
		const summary = vi.fn(), cell = vi.fn();
		bus.subscribeAll(summary);
		bus.subscribe('000001', cell);
		FakeEventSource.instances[0].emit(frame());
		expect(summary).toHaveBeenCalledOnce();
		expect(cell).not.toHaveBeenCalled();
		expect(bus.getVersion()).toBeGreaterThan(0);
		off();
	});

	it('reports disconnect, degradation and recovery without fabricating a quote', () => {
		const bus = createMonitorLiveBus();
		const off = bus.connect(config);
		const source = FakeEventSource.instances[0];
		source.onopen?.();
		expect(bus.getStatus().connection).toBe('open');
		source.onerror?.();
		expect(bus.getStatus()).toMatchObject({ connection: 'reconnecting', stale: true });
		source.emit({ ticks: null, status: { snapshot_stale: true, snapshot_source: 'fixture', snapshot_warning: 'partial snapshot', session_open: true } });
		expect(bus.getStatus()).toMatchObject({ connection: 'open', stale: true, source: 'fixture', warning: 'partial snapshot' });
		expect(bus.get('600000')).toBeUndefined();
		source.emit({ ...frame(), status: { snapshot_stale: false, session_open: false } });
		expect(bus.getStatus()).toMatchObject({ stale: false, warning: '' });
		off();
	});

	it('marks an individually expired symbol and refreshes it even when the price is unchanged', () => {
		vi.useFakeTimers();
		vi.setSystemTime(1000000);
		const bus = createMonitorLiveBus();
		const off = bus.connect(config);
		const source = FakeEventSource.instances[0];
		const cell = vi.fn();
		bus.subscribe('600000', cell);
		source.emit({ ...frame(12.34, 1.25, Date.now()), status: { session_open: true } });
		vi.advanceTimersByTime(75000);
		expect(bus.get('600000')?.stale).toBe(true);
		expect(bus.getStatus().stale).toBe(true);
		source.emit({ ...frame(12.34, 1.25, Date.now()), status: { session_open: true, snapshot_stale: false } });
		expect(bus.get('600000')?.stale).toBeUndefined();
		expect(cell).toHaveBeenCalledTimes(3);
		off();
		expect(vi.getTimerCount()).toBe(0);
	});
	it('shares one authenticated EventSource and closes after the last consumer leaves', () => {
		const bus = createMonitorLiveBus();
		const firstOff = bus.connect(config);
		const secondOff = bus.connect(config);
		expect(FakeEventSource.instances).toHaveLength(1);
		const source = FakeEventSource.instances[0];
		expect(source.url).toBe('http://127.0.0.1:20081/api/v1/monitor/stream?token=local-token');
		firstOff();
		firstOff();
		expect(source.close).not.toHaveBeenCalled();
		secondOff();
		expect(source.close).toHaveBeenCalledOnce();
	});

	it('matches full, prefixed and six-digit symbols and only notifies affected cells', () => {
		const bus = createMonitorLiveBus();
		const off = bus.connect(config);
		const changed = vi.fn(), untouched = vi.fn();
		bus.subscribe('sh600000', changed);
		bus.subscribe('000001.SZ', untouched);
		FakeEventSource.instances[0].emit(frame());
		expect(bus.get('600000')?.price).toBe(12.34);
		expect(bus.get('600000.SH')).toBe(bus.get('sh600000'));
		expect(changed).toHaveBeenCalledOnce();
		expect(untouched).not.toHaveBeenCalled();
		off();
	});

	it('ignores unchanged prices and rejects late frames using the newest timestamp', () => {
		const bus = createMonitorLiveBus();
		const off = bus.connect(config);
		const changed = vi.fn();
		bus.subscribe('600000', changed);
		const source = FakeEventSource.instances[0];
		source.emit(frame(12.34, 1.25, 100));
		source.emit(frame(12.34, 1.25, 200));
		source.emit(frame(99, 50, 150));
		expect(changed).toHaveBeenCalledOnce();
		expect(bus.get('600000')).toEqual({ price: 12.34, changePercent: 1.25, ts: 200 });
		off();
	});

	it('does not invent values for empty, malformed or invalid payloads', () => {
		const bus = createMonitorLiveBus();
		const off = bus.connect(config);
		const source = FakeEventSource.instances[0];
		for (const data of ['{broken', 'null', {}, { ticks: [] }, frame(0), frame(-1), frame(12, 0, 0)]) source.emit(data);
		expect(bus.get('600000')).toBeUndefined();
		source.emit(frame(12, 0));
		expect(bus.get('600000')?.changePercent).toBe(0);
		off();
	});

	it('replaces a changed backend and discards events from the old connection', () => {
		const bus = createMonitorLiveBus();
		const oldOff = bus.connect(config);
		const oldSource = FakeEventSource.instances[0];
		oldSource.emit(frame());
		const newOff = bus.connect({ ...config, backendUrl: 'http://127.0.0.1:20082' });
		expect(oldSource.close).toHaveBeenCalledOnce();
		expect(bus.get('600000')).toBeUndefined();
		oldSource.emit(frame(99));
		expect(bus.get('600000')).toBeUndefined();
		oldOff();
		expect(FakeEventSource.instances[1].close).not.toHaveBeenCalled();
		newOff();
	});
});

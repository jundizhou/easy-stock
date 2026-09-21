import { beforeEach, describe, expect, it, vi } from 'vitest';
import { identifyLadderThemes, LadderThemeEntry, LadderThemeStock } from './ladder-theme-ai';
import { BackendConfig, requestJSON } from './backend';
vi.mock('./backend', () => ({ requestJSON: vi.fn() }));
const request = vi.mocked(requestJSON);
const config = {backendUrl:'http://localhost:1234',token:''} as BackendConfig;
const stock = {trade_date:'2026-09-18',symbol:'300285.SZ',name:'国瓷材料',raw_concepts:[],theme_evidence:[]} as unknown as LadderThemeStock;
const success: LadderThemeEntry = {symbol:stock.symbol,name:stock.name,status:'success',last_attempt:'2020-01-01',result:{themes:['电子陶瓷'],reason:'示例',basis:'web',caveat:'',sources:[{title:'报道',url:'https://example.com/news',date:'2026-09-18',snippet:'题材报道'}],confidence:'medium'}};
beforeEach(() => { request.mockReset(); });
describe('ladder theme scheduling', () => {
	it('reuses old success without another model request',async () => {
		request.mockResolvedValue({data:{[stock.symbol]:success}});
		await identifyLadderThemes(config,[stock],false,new AbortController().signal,vi.fn());
		expect(request).toHaveBeenCalledTimes(1);
	});
	it('manual refresh bypasses success cache and deduplicates symbols',async () => {
		request.mockResolvedValueOnce({data:{[stock.symbol]:success}}).mockResolvedValueOnce({data:success});
		await identifyLadderThemes(config,[stock,stock],true,new AbortController().signal,vi.fn());
		expect(request).toHaveBeenCalledTimes(2);
		expect(JSON.parse(request.mock.calls[1][2]!.body as string).force).toBe(true);
	});
	it('keeps current evidence when yesterday contains the same stock', async () => {
		request.mockResolvedValueOnce({data:{}}).mockResolvedValueOnce({data:success});
		await identifyLadderThemes(config,[{...stock,primary_theme:'电子陶瓷'},{...stock,primary_theme:'旧题材'}],false,new AbortController().signal,vi.fn());
		expect(JSON.parse(request.mock.calls[1][2]!.body as string)).toMatchObject({existing_theme:'电子陶瓷',trade_date:'2026-09-18'});
	});
	it('submits at most two stocks and stops queued work when disabled', async () => {
		vi.useFakeTimers();
		try {
			const controller = new AbortController();
			request.mockResolvedValueOnce({data:{}}).mockResolvedValue({data:{...success,status:'running'}});
			const stocks = [stock,{...stock,symbol:'000001.SZ'},{...stock,symbol:'000002.SZ'}];
			const pending = identifyLadderThemes(config,stocks,false,controller.signal,vi.fn());
			const stopped = expect(pending).rejects.toThrow('aborted');
			await vi.advanceTimersByTimeAsync(0);
			expect(request.mock.calls.filter(call => call[2]?.method === 'POST')).toHaveLength(2);
			controller.abort();
			await stopped;
			expect(request).toHaveBeenCalledTimes(3);
		} finally {
			vi.useRealTimers();
		}
	});
	it('stops scheduling after disabling',async () => {
		const controller = new AbortController();
		request.mockImplementationOnce(async () => {controller.abort();return {data:{}};});
		await identifyLadderThemes(config,[stock],false,controller.signal,vi.fn());
		expect(request).toHaveBeenCalledTimes(1);
	});
	it('shows cooldown failures without polling or retrying',async () => {
		request.mockResolvedValueOnce({data:{}}).mockResolvedValueOnce({data:{...success,status:'failed',result:undefined}});
		const update=vi.fn();
		await identifyLadderThemes(config,[stock],false,new AbortController().signal,update);
		expect(request).toHaveBeenCalledTimes(2);
		expect(update.mock.lastCall?.[0][stock.symbol].status).toBe('failed');
	});
});

describe('full selection AI scheduling', () => {
 it('processes all twenty stocks without a six-stock limit', async () => {
  request.mockResolvedValueOnce({data:{}}).mockResolvedValue({data:success});
  const stocks = Array.from({length:20}, (_, i) => ({...stock,symbol:`${String(i).padStart(6,'0')}.SZ`,streak:20-i}));
  const progress=vi.fn();
  await identifyLadderThemes(config,stocks,false,new AbortController().signal,vi.fn(),progress);
  expect(request.mock.calls.filter(call => call[2]?.method === 'POST')).toHaveLength(20);
  expect(progress.mock.lastCall?.[0]).toEqual({total:20,completed:20,active:[],remaining:0});
 });
 it('skips failed stocks during cooldown without submitting requests', async () => {
  request.mockResolvedValueOnce({data:{[stock.symbol]:{...success,result:undefined,status:'failed',last_attempt:new Date().toISOString()}}});
  await identifyLadderThemes(config,[stock],false,new AbortController().signal,vi.fn());
  expect(request).toHaveBeenCalledTimes(1);
 });
 it('stops scheduling after three failures instead of draining the list', async () => {
  request.mockResolvedValueOnce({data:{}}).mockResolvedValue({data:{...success,result:undefined,status:'failed'}});
  const stocks = Array.from({length:20}, (_, i) => ({...stock,symbol:`${String(i).padStart(6,'0')}.SZ`}));
  await expect(identifyLadderThemes(config,stocks,false,new AbortController().signal,vi.fn())).rejects.toThrow('三只');
  // At most one additional request was already in flight when the third failed.
  expect(request.mock.calls.filter(call => call[2]?.method === 'POST').length).toBeLessThanOrEqual(4);
 });
 it('pauses after two minutes without any completed stock', async () => {
  vi.useFakeTimers();
  try {
   request.mockResolvedValueOnce({data:{}}).mockImplementation(async (_config, _path, options) => options?.method === 'POST'
    ? {data:{...success,status:'running'}} : {data:{[stock.symbol]:{...success,status:'running'}}});
   const pending = identifyLadderThemes(config,[stock],false,new AbortController().signal,vi.fn());
   const ended = expect(pending).rejects.toThrow('两分钟');
   await vi.advanceTimersByTimeAsync(120_000);
   await ended;
   expect(vi.getTimerCount()).toBe(0);
  } finally { vi.useRealTimers(); }
 });
});

it('continues beyond two minutes while stocks keep completing', async () => {
 vi.useFakeTimers();
 try {
  const stocks = Array.from({length:8}, (_, i) => ({...stock,symbol:`${String(i).padStart(6,'0')}.SZ`}));
  request.mockResolvedValueOnce({data:{}}).mockImplementation(async () => {
   await new Promise(resolve => setTimeout(resolve, 60_000));
   return {data:success};
  });
  const progress = vi.fn();
  const pending = identifyLadderThemes(config,stocks,false,new AbortController().signal,vi.fn(),progress);
  await vi.advanceTimersByTimeAsync(240_000);
  await pending;
  expect(request.mock.calls.filter(call => call[2]?.method === 'POST')).toHaveLength(8);
  expect(progress.mock.lastCall?.[0]).toEqual({total:8,completed:8,active:[],remaining:0});
  expect(vi.getTimerCount()).toBe(0);
 } finally { vi.useRealTimers(); }
});

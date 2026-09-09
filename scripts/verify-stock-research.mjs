import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';

const base = process.env.RESEARCH_API_URL || 'http://127.0.0.1:20081';
const output = path.resolve(process.env.RESEARCH_OUTPUT_DIR || '.runtime/research-evaluation');
const symbols = process.argv.slice(2);
const cases = (symbols.length ? symbols : ['600519', '300750', '601138', '688981', '600770']).map((symbol) => ({ symbol, purpose: 'observe', horizon: symbol === '600770' ? 'short' : 'swing' }));
const headers = { 'Content-Type': 'application/json', ...(process.env.A_STOCK_TOKEN ? { Authorization: `Bearer ${process.env.A_STOCK_TOKEN}` } : {}) };
async function request(route, options = {}) {
  const response = await fetch(`${base}/api/v1/stocks/research${route}`, { ...options, headers, signal: AbortSignal.timeout(30_000) });
  const result = await response.json();
  if (!response.ok) throw new Error(`${response.status}: ${result.error || JSON.stringify(result)}`);
  return result.data;
}
function audit(job, snapshot) {
  const errors = [];
  const check = (label, fn) => { try { fn(); } catch (error) { errors.push(`${label}: ${error.message}`); } };
  const report = job.analysis?.research_report;
  check('AI completion', () => assert.equal(job.status, 'succeeded'));
  if (!report) {
    const failClosed = !job.analysis || (job.analysis.risk_control.stop_price === 0 && job.analysis.risk_control.take_profit_first === 0 && job.analysis.risk_control.suggested_position_max_percent === 0);
    check('degraded advice cleared', () => assert.ok(failClosed));
    return { errors, fail_closed: failClosed, limitation: 'No AI report; must not be counted as an accurate analysis.' };
  }
  check('baseline preserved', () => assert.deepEqual(job.analysis.scorecard, snapshot.rule_baseline));
  check('snapshot identity', () => { assert.equal(report.snapshot_id, snapshot.id); assert.equal(report.snapshot_version, snapshot.version); });
  check('bounded calls', () => assert.ok(report.attempts.length >= 2 && report.attempts.length <= 3));
  check('bounded supplements', () => assert.ok(report.questions.length <= 3));
  const sources = new Map(report.sources.map((source) => [source.id, source]));
  const claims = [report.thesis, ...report.support, ...report.counter, ...report.alternatives];
  for (const claim of claims) check(`citation ${claim.text.slice(0, 24)}`, () => {
    assert.ok(claim.source_ids.length > 0);
    for (const id of claim.source_ids) assert.ok(sources.has(id), id);
    if (claim.quote) assert.ok(claim.source_ids.some((id) => sources.get(id)?.content.includes(claim.quote)));
  });
  check('publication cutoff', () => {
    for (const source of report.sources) if (source.time_status === 'dated' && source.published_at) assert.ok(Date.parse(source.published_at) <= Date.parse(snapshot.cutoff_at));
  });
  const anchors = new Map(report.anchors.map((anchor) => [anchor.id, anchor.price]));
  for (const condition of report.conditions) if (condition.metric === 'close') check(`condition ${condition.id}`, () => assert.equal(condition.threshold, anchors.get(condition.anchor_id)));
  const plan = report.decision.price_plan;
  check('execution prices', () => {
    if (!plan) { assert.equal(job.analysis.risk_control.stop_price, 0); assert.equal(job.analysis.risk_control.take_profit_first, 0); return; }
    assert.equal(report.decision.status, 'conditional');
    assert.equal(job.analysis.risk_control.entry_reference, anchors.get(plan.entry_anchor));
    assert.equal(job.analysis.risk_control.stop_price, anchors.get(plan.stop_anchor));
    assert.ok(anchors.get(plan.stop_anchor) < anchors.get(plan.entry_anchor));
    if (plan.target_anchor) assert.ok(anchors.get(plan.target_anchor) > anchors.get(plan.entry_anchor));
  });
  return { errors, claims_checked: claims.length, evidence_level: report.evidence_level, decision: report.decision.status, model_calls: report.attempts.length, limitation: 'Structural checks only. Claims require independent source review; future scenarios are not accuracy scores.' };
}

await fs.mkdir(output, { recursive: true });
const evaluations = [];
for (const input of cases) {
  console.log(`Starting ${input.symbol}`);
  const started = Date.now();
  let job;
  try {
    job = await request('', { method: 'POST', body: JSON.stringify(input) });
    while (['queued', 'running'].includes(job.status)) {
      if (Date.now() - started > 14 * 60_000) throw new Error('Evaluation wait deadline exceeded; inspect persisted job');
      await new Promise((resolve) => setTimeout(resolve, 2500));
      job = await request(`/${job.id}`);
    }
    const snapshot = await request(`/${job.id}/snapshot`);
    const validation = audit(job, snapshot);
    await fs.writeFile(path.join(output, `${input.symbol}.json`), JSON.stringify({ job, snapshot, validation }, null, 2));
    evaluations.push({ symbol: input.symbol, name: job.analysis?.name, job_id: job.id, status: job.status, duration_seconds: Math.round((Date.now() - started) / 1000), ...validation });
    console.log(`${input.symbol}: ${job.status}, ${validation.errors.length} structural failures`);
  } catch (error) {
    evaluations.push({ symbol: input.symbol, job_id: job?.id, errors: [error.message] });
    console.error(`${input.symbol}: ${error.message}`);
  }
  await fs.writeFile(path.join(output, 'summary.json'), JSON.stringify({ checked_at: new Date().toISOString(), cases: evaluations }, null, 2));
}
if (evaluations.some((item) => item.errors.length)) process.exitCode = 1;

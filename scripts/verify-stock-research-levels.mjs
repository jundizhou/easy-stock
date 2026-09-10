import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';

const base = process.env.RESEARCH_API_URL || 'http://127.0.0.1:20081';
const output = path.resolve(process.env.RESEARCH_OUTPUT_DIR || '.runtime/research-level-evaluation');
const symbol = process.argv[2] || '000930.SZ';
const levels = process.argv.slice(3);
const policies = {
  quantitative: { calls: 0, timeout: 120_000 },
  quick: { calls: 1, timeout: 180_000, bytes: 4_000 },
  standard: { calls: 2, timeout: 360_000, bytes: 8_000 },
  deep: { calls: 3, timeout: 1_080_000 },
};
const headers = { 'Content-Type': 'application/json', ...(process.env.A_STOCK_TOKEN ? { Authorization: `Bearer ${process.env.A_STOCK_TOKEN}` } : {}) };
async function request(route, options = {}) {
  const response = await fetch(`${base}/api/v1/${route}`, { ...options, headers, signal: AbortSignal.timeout(30_000) });
  const result = await response.json();
  if (!response.ok) throw new Error(`${response.status}: ${result.error || JSON.stringify(result)}`);
  return result.data;
}
const usage = async () => (await request('settings/token-usage?module=stock-analysis')).total;
function audit(job, snapshot, policy, tokens) {
  const errors = [];
  const check = (label, fn) => { try { fn(); } catch (error) { errors.push(`${label}: ${error.message}`); } };
  const analysis = job.analysis;
  const report = analysis?.research_report;
  check('job completion', () => assert.equal(job.status, 'succeeded', job.error || job.message));
  check('baseline preserved', () => assert.deepEqual(analysis?.scorecard, snapshot.rule_baseline));
  if (!policy.calls) {
    check('AI not called', () => { assert.notEqual(analysis?.ai.status, 'ready'); assert.ok(!report); assert.equal(tokens.total_tokens, 0); });
    return { errors, model_calls: 0 };
  }
  check('AI completion', () => { assert.equal(analysis?.ai.status, 'ready'); assert.ok(report); });
  if (!report) {
    check('failure clears executable advice', () => {
      assert.equal(analysis?.risk_control.stop_price, 0);
      assert.equal(analysis?.risk_control.take_profit_first, 0);
      assert.equal(analysis?.risk_control.suggested_position_max_percent, 0);
    });
    return { errors, model_calls: null };
  }
  check('request level', () => assert.equal(report.analysis_level, job.request.analysis_level));
  check('call count', () => assert.ok(report.attempts.length === policy.calls || (job.request.analysis_level === 'deep' && report.attempts.length === 4) || (job.request.analysis_level === 'standard' && report.attempts.length === 3)));
  check('compression version', () => assert.equal(report.compression.version, 'evidence-pack-v2'));
  if (policy.bytes) check('evidence budget', () => assert.ok(report.compression.selected_content_bytes <= policy.bytes));
  check('snapshot identity', () => { assert.equal(report.snapshot_id, snapshot.id); assert.equal(report.snapshot_version, snapshot.version); });
  check('nonempty judgment and actions', () => {
    for (const text of [report.headline, report.thesis.text, report.main_conflict, report.decision.reason, report.decision.new_position, report.decision.existing_position]) assert.ok(text?.trim());
  });
  const sources = new Map(report.sources.map((source) => [source.id, source]));
  const claims = [report.thesis, ...report.support, ...report.counter, ...report.alternatives];
  for (const claim of claims) check(`citation ${claim.text.slice(0, 24)}`, () => {
    assert.ok(claim.source_ids.length);
    for (const id of claim.source_ids) assert.ok(sources.has(id), id);
    if (claim.quote) assert.ok(claim.source_ids.some((id) => sources.get(id)?.content.includes(claim.quote)));
  });
  const anchors = new Map(report.anchors.map((anchor) => [anchor.id, anchor.price]));
  for (const condition of report.conditions) check(`condition ${condition.id}`, () => {
    for (const id of condition.source_ids) assert.ok(sources.has(id), id);
    if (condition.metric === 'close') assert.equal(condition.threshold, anchors.get(condition.anchor_id));
  });
  const plan = report.decision.price_plan;
  check('execution prices', () => {
    if (!plan) { assert.equal(analysis.risk_control.stop_price, 0); assert.equal(analysis.risk_control.take_profit_first, 0); return; }
    assert.equal(report.decision.status, 'conditional');
    assert.equal(analysis.risk_control.entry_reference, anchors.get(plan.entry_anchor));
    assert.equal(analysis.risk_control.stop_price, anchors.get(plan.stop_anchor));
    assert.ok(anchors.get(plan.stop_anchor) < anchors.get(plan.entry_anchor));
    if (plan.target_anchor) assert.ok(anchors.get(plan.target_anchor) > anchors.get(plan.entry_anchor));
  });
  check('publication cutoff', () => {
    for (const source of report.sources) if (source.time_status === 'dated' && source.published_at) assert.ok(Date.parse(source.published_at) <= Date.parse(snapshot.cutoff_at));
  });
  if (job.request.analysis_level === 'quick') check('quick has no trading plan', () => {
    assert.ok(['observe', 'no_plan'].includes(report.decision.status)); assert.ok(!plan); assert.equal(report.conditions.length, 0);
  });
  return { errors, model_calls: report.attempts.length, claims_checked: claims.length, evidence_level: report.evidence_level, decision: report.decision.status, attempts: report.attempts, compression: report.compression };
}

await fs.mkdir(output, { recursive: true });
const evaluations = [];
for (const level of levels.length ? levels : Object.keys(policies)) {
  const policy = policies[level];
  assert.ok(policy, `Unknown level ${level}`);
  let job;
  const started = Date.now();
  try {
    const before = await usage();
    job = await request('stocks/research', { method: 'POST', body: JSON.stringify({ symbol, purpose: 'observe', horizon: 'swing', analysis_level: level }) });
    console.log(`${level}: started ${job.id}`);
    let lastMessage;
    while (['queued', 'running'].includes(job.status)) {
      if (Date.now() - started > policy.timeout + 60_000) throw new Error('Evaluation wait deadline exceeded');
      if (job.message !== lastMessage) { console.log(`${level}: ${job.message}`); lastMessage = job.message; }
      await new Promise((resolve) => setTimeout(resolve, 2500));
      job = await request(`stocks/research/${job.id}`);
    }
    const snapshot = await request(`stocks/research/${job.id}/snapshot`);
    const after = await usage();
    const tokens = Object.fromEntries(['prompt_tokens', 'completion_tokens', 'total_tokens'].map((key) => [key, after[key] - before[key]]));
    const validation = audit(job, snapshot, policy, tokens);
    const durationMS = Date.parse(job.completed_at) - Date.parse(job.started_at);
    const evaluation = { level, symbol, name: job.analysis?.name, job_id: job.id, model: job.analysis?.ai.model, status: job.status, duration_ms: durationMS, tokens, ...validation };
    await fs.writeFile(path.join(output, `${symbol}-${level}.json`), JSON.stringify({ job, snapshot, validation: evaluation }, null, 2));
    evaluations.push(evaluation);
    console.log(JSON.stringify(evaluation));
  } catch (error) {
    evaluations.push({ level, symbol, job_id: job?.id, errors: [error.message] });
    console.error(`${level}: ${error.message}`);
  }
  await fs.writeFile(path.join(output, 'summary.json'), JSON.stringify({ checked_at: new Date().toISOString(), note: 'Token values are application usage deltas and may be estimates when the provider omits usage. Structure checks are not prediction accuracy.', cases: evaluations }, null, 2));
}
if (evaluations.some((item) => item.errors.length)) process.exitCode = 1;

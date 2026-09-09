import fs from 'node:fs/promises';
import path from 'node:path';

const directory = path.resolve(process.argv[2] || '.runtime/research-evaluation-final');
const reference = JSON.parse(await fs.readFile(process.argv[3] || 'docs/verification/stock-research-2026-09-08.json', 'utf8'));
const cases = [];
for (const expected of reference.cases) {
  const captured = JSON.parse(await fs.readFile(path.join(directory, `${expected.symbol}.json`), 'utf8'));
  const snapshot = captured.snapshot;
  const source = snapshot.sources.find((item) => item.id === 'f-financial');
  const payload = JSON.parse(source.content);
  const financial = payload.data || payload;
  const checks = {
    quote_date: snapshot.quote.trade_time?.startsWith(reference.quote_date) === true,
    close: Math.abs(snapshot.quote.price - expected.close) < 0.005,
    report_period: financial.report_date.startsWith(reference.report_date),
    revenue: Math.abs(financial.revenue - expected.revenue) <= reference.amount_tolerance,
    net_profit: Math.abs(financial.net_profit - expected.net_profit) <= reference.amount_tolerance,
    deducted_net_profit: Math.abs(financial.deducted_net_profit - expected.deducted_net_profit) <= reference.amount_tolerance,
  };
  const analysis = captured.job.analysis;
  const failClosed = analysis.research_report ? null : analysis.risk_control.stop_price === 0 && analysis.risk_control.take_profit_first === 0 && analysis.risk_control.suggested_position_max_percent === 0;
  cases.push({ symbol: expected.symbol, name: expected.name, checks, passed: Object.values(checks).every(Boolean), ai_status: captured.job.status, degraded_advice_cleared: failClosed, pitfall: expected.pitfall });
}
const result = { checked_at: new Date().toISOString(), reference_source: reference.reference_source, cases, limitation: 'Only independently checked market/financial inputs. This is NOT AI semantic accuracy, forecast accuracy or investment return validation.' };
await fs.writeFile(path.join(directory, 'facts-check.json'), JSON.stringify(result, null, 2));
console.log(JSON.stringify(result, null, 2));
if (cases.some((item) => !item.passed || item.degraded_advice_cleared === false)) process.exitCode = 1;

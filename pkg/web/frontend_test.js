const test = require('node:test');
const assert = require('node:assert');
const dashboard = require('./static/dashboard.js');

test('formatCredits formats numbers correctly', () => {
  assert.strictEqual(dashboard.formatCredits(0), '0.00');
  assert.strictEqual(dashboard.formatCredits(1234.5), '1,234.50');
  assert.strictEqual(dashboard.formatCredits(1500), '1,500.00');
  assert.strictEqual(dashboard.formatCredits(3.7524), '3.75');
  assert.strictEqual(dashboard.formatCredits(null), '0.00');
  assert.strictEqual(dashboard.formatCredits(undefined), '0.00');
});

test('formatPercent formats percentage correctly', () => {
  assert.strictEqual(dashboard.formatPercent(0), '0.0%');
  assert.strictEqual(dashboard.formatPercent(45.678), '45.7%');
  assert.strictEqual(dashboard.formatPercent(100), '100.0%');
  assert.strictEqual(dashboard.formatPercent(null), '0.0%');
});

test('calculatePacing computes projections and statuses', () => {
  // On track: 300 credits used on day 10 of 30, quota 1500 -> projected 900
  const onTrack = dashboard.calculatePacing(300, 10, 30, 1500);
  assert.strictEqual(onTrack.status, 'On Track');
  assert.strictEqual(onTrack.badgeClass, 'badge-success');
  assert.strictEqual(onTrack.daysRemaining, 20);
  assert.strictEqual(onTrack.actualDailyAvg, 30);
  assert.strictEqual(onTrack.targetDailyAvg, 50);

  // Near quota: 950 credits used on day 20 of 30, quota 1500 -> projected 1425 (95%)
  const nearQuota = dashboard.calculatePacing(950, 20, 30, 1500);
  assert.strictEqual(nearQuota.status, 'Near Quota Pace');
  assert.strictEqual(nearQuota.badgeClass, 'badge-warning');

  // Over budget: 800 credits used on day 10 of 30, quota 1500 -> projected 2400
  const overBudget = dashboard.calculatePacing(800, 10, 30, 1500);
  assert.strictEqual(overBudget.status, 'Over Budget Pace');
  assert.strictEqual(overBudget.badgeClass, 'badge-danger');

  // Quota exceeded: 1600 credits used
  const exceeded = dashboard.calculatePacing(1600, 15, 30, 1500);
  assert.strictEqual(exceeded.status, 'Quota Exceeded');
  assert.strictEqual(exceeded.badgeClass, 'badge-danger');
});

test('buildChartConfig generates valid Chart.js config', () => {
  const summary = {
    year: 2026,
    month: 9,
    days_in_month: 3,
    today_day: 2,
    quota: 1500,
    daily_usage: [
      { day: 1, credits: 10, cumulative_credits: 10, is_today: false, is_future: false },
      { day: 2, credits: 15, cumulative_credits: 25, is_today: true, is_future: false },
      { day: 3, credits: 0, cumulative_credits: null, is_today: false, is_future: true }
    ]
  };

  const config = dashboard.buildChartConfig(summary);
  assert.strictEqual(config.type, 'line');
  assert.deepStrictEqual(config.data.labels, ['Day 1', 'Day 2', 'Day 3']);

  // Cumulative dataset
  const cumDataset = config.data.datasets.find(d => d.label === 'Cumulative Usage');
  assert.ok(cumDataset);
  assert.deepStrictEqual(cumDataset.data, [10, 25, null]);

  // Daily dataset
  const dailyDataset = config.data.datasets.find(d => d.label === 'Daily Usage');
  assert.ok(dailyDataset);
  assert.deepStrictEqual(dailyDataset.data, [10, 15, null]);

  // Quota dataset
  const quotaDataset = config.data.datasets.find(d => d.label === 'Monthly Quota');
  assert.ok(quotaDataset);
  assert.deepStrictEqual(quotaDataset.data, [1500, 1500, 1500]);
});

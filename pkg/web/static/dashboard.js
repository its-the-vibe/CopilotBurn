/**
 * CopilotBurn Frontend Dashboard Logic
 */

// Helper: Format credit number to 2 decimal places with thousands separators
function formatCredits(num) {
  if (num === null || num === undefined || isNaN(num)) return '0.00';
  return Number(num).toLocaleString('en-US', {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2
  });
}

// Helper: Format percentage
function formatPercent(num) {
  if (num === null || num === undefined || isNaN(num)) return '0.0%';
  return Number(num).toFixed(1) + '%';
}

// Helper: Calculate burn rate pacing and projection
function calculatePacing(monthlyTotal, todayDay, daysInMonth, quota) {
  const total = Number(monthlyTotal) || 0;
  const daysPassed = Math.max(1, Number(todayDay) || 1);
  const totalDays = Math.max(1, Number(daysInMonth) || 30);
  const q = Number(quota) || 1500;

  const actualDailyAvg = total / daysPassed;
  const targetDailyAvg = q / totalDays;
  const projectedMonthTotal = actualDailyAvg * totalDays;
  const daysRemaining = Math.max(0, totalDays - daysPassed);

  let status = 'On Track';
  let badgeClass = 'badge-success';

  if (total > q) {
    status = 'Quota Exceeded';
    badgeClass = 'badge-danger';
  } else if (projectedMonthTotal > q * 1.1) {
    status = 'Over Budget Pace';
    badgeClass = 'badge-danger';
  } else if (projectedMonthTotal > q * 0.9) {
    status = 'Near Quota Pace';
    badgeClass = 'badge-warning';
  }

  return {
    actualDailyAvg,
    targetDailyAvg,
    projectedMonthTotal,
    daysRemaining,
    status,
    badgeClass
  };
}

// Helper: Build Chart.js configuration from backend summary data
function buildChartConfig(summary) {
  const labels = [];
  const cumulativeData = [];
  const dailyData = [];
  const quotaLine = [];
  const targetPaceLine = [];

  const daysInMonth = summary.days_in_month || 30;
  const quota = summary.quota || 1500;
  const todayDay = summary.today_day || 0;
  const dailyList = summary.daily_usage || [];

  const targetStep = quota / daysInMonth;

  for (let d = 1; d <= daysInMonth; d++) {
    labels.push(`Day ${d}`);
    quotaLine.push(quota);
    targetPaceLine.push(Number((targetStep * d).toFixed(2)));

    const item = dailyList[d - 1];
    if (item && !item.is_future && item.cumulative_credits !== null && item.cumulative_credits !== undefined) {
      cumulativeData.push(item.cumulative_credits);
      dailyData.push(item.credits);
    } else {
      cumulativeData.push(null);
      dailyData.push(null);
    }
  }

  return {
    type: 'line',
    data: {
      labels: labels,
      datasets: [
        {
          label: 'Cumulative Usage',
          data: cumulativeData,
          borderColor: '#6366f1',
          backgroundColor: 'rgba(99, 102, 241, 0.12)',
          fill: true,
          tension: 0.2,
          borderWidth: 3,
          pointRadius: function (context) {
            const index = context.dataIndex;
            return index + 1 === todayDay ? 6 : 3;
          },
          pointHoverRadius: 7,
          pointBackgroundColor: function (context) {
            const index = context.dataIndex;
            return index + 1 === todayDay ? '#10b981' : '#6366f1';
          },
          pointBorderColor: '#ffffff',
          pointBorderWidth: 1.5,
          yAxisID: 'y'
        },
        {
          label: 'Daily Usage',
          type: 'bar',
          data: dailyData,
          backgroundColor: 'rgba(14, 165, 233, 0.45)',
          borderColor: '#0ea5e9',
          borderWidth: 1,
          borderRadius: 4,
          yAxisID: 'y1'
        },
        {
          label: 'Monthly Quota',
          data: quotaLine,
          borderColor: '#ef4444',
          borderDash: [6, 6],
          borderWidth: 2,
          fill: false,
          pointRadius: 0,
          yAxisID: 'y'
        },
        {
          label: 'Target Pace',
          data: targetPaceLine,
          borderColor: '#9ca3af',
          borderDash: [3, 3],
          borderWidth: 1.5,
          fill: false,
          pointRadius: 0,
          yAxisID: 'y'
        }
      ]
    },
    options: {
      responsive: true,
      maintainAspectRatio: false,
      interaction: {
        mode: 'index',
        intersect: false
      },
      plugins: {
        legend: {
          display: true,
          labels: {
            color: '#9ca3af',
            usePointStyle: true,
            boxWidth: 8
          }
        },
        tooltip: {
          backgroundColor: '#1f2937',
          titleColor: '#f9fafb',
          bodyColor: '#e5e7eb',
          borderColor: '#374151',
          borderWidth: 1,
          padding: 10,
          callbacks: {
            label: function (context) {
              const datasetLabel = context.dataset.label || '';
              const val = context.parsed.y;
              if (val === null || val === undefined) return null;
              if (datasetLabel === 'Cumulative Usage') {
                const pct = ((val / quota) * 100).toFixed(1);
                return `${datasetLabel}: ${formatCredits(val)} credits (${pct}% of quota)`;
              }
              return `${datasetLabel}: ${formatCredits(val)} credits`;
            }
          }
        }
      },
      scales: {
        x: {
          grid: {
            color: 'rgba(55, 65, 81, 0.4)'
          },
          ticks: {
            color: '#9ca3af',
            maxRotation: 0,
            autoSkip: true,
            maxTicksLimit: 15
          }
        },
        y: {
          type: 'linear',
          display: true,
          position: 'left',
          title: {
            display: true,
            text: 'Cumulative Credits',
            color: '#9ca3af',
            font: { size: 11 }
          },
          grid: {
            color: 'rgba(55, 65, 81, 0.4)'
          },
          ticks: {
            color: '#9ca3af',
            callback: function (val) {
              return Number(val).toLocaleString();
            }
          }
        },
        y1: {
          type: 'linear',
          display: true,
          position: 'right',
          title: {
            display: true,
            text: 'Daily Credits',
            color: '#9ca3af',
            font: { size: 11 }
          },
          grid: {
            drawOnChartArea: false
          },
          ticks: {
            color: '#0ea5e9',
            callback: function (val) {
              return Number(val).toLocaleString();
            }
          }
        }
      }
    }
  };
}

// Frontend Controller for browser environment
let chartInstance = null;
let currentYear = null;
let currentMonth = null;
let refreshIntervalTimer = null;

async function fetchUsageData(year, month) {
  let url = '/api/usage';
  const params = [];
  if (year) params.push(`year=${year}`);
  if (month) params.push(`month=${month}`);
  if (params.length > 0) {
    url += '?' + params.join('&');
  }

  const response = await fetch(url);
  if (!response.ok) {
    let errDetail = 'HTTP error ' + response.status;
    try {
      const errJson = await response.json();
      if (errJson && errJson.error) errDetail = errJson.error;
    } catch (_) {}
    throw new Error(errDetail);
  }
  return await response.json();
}

function updateDashboardDOM(summary) {
  currentYear = summary.year;
  currentMonth = summary.month;

  // Month navigation label
  const monthLabel = document.getElementById('currentMonthLabel');
  if (monthLabel) {
    monthLabel.textContent = `${summary.month_name} ${summary.year}`;
  }

  // Monthly Total & Quota
  const monthlyValEl = document.getElementById('monthlyTotalVal');
  const quotaValEl = document.getElementById('quotaTotalVal');
  const configuredQuotaValEl = document.getElementById('configuredQuotaVal');
  if (monthlyValEl) monthlyValEl.textContent = formatCredits(summary.monthly_total);
  if (quotaValEl) quotaValEl.textContent = formatCredits(summary.quota);
  if (configuredQuotaValEl) configuredQuotaValEl.textContent = formatCredits(summary.quota);

  // Progress Bar
  const pct = Math.min(100, Math.max(0, summary.percentage_used || 0));
  const fillEl = document.getElementById('quotaProgressFill');
  const barContainer = document.getElementById('quotaProgressBar');
  if (fillEl && barContainer) {
    fillEl.style.width = `${pct}%`;
    barContainer.setAttribute('aria-valuenow', pct.toString());

    if (pct > 90) {
      fillEl.style.backgroundColor = 'var(--color-danger)';
    } else if (pct > 70) {
      fillEl.style.backgroundColor = 'var(--color-warning)';
    } else {
      fillEl.style.backgroundColor = 'var(--brand-primary)';
    }
  }

  const pctTextEl = document.getElementById('quotaPercentText');
  const remTextEl = document.getElementById('quotaRemainingText');
  if (pctTextEl) pctTextEl.textContent = `${formatPercent(summary.percentage_used)} used`;
  if (remTextEl) remTextEl.textContent = `${formatCredits(summary.remaining_quota)} credits remaining`;

  // Today's Usage (Highlighted & Distinguished)
  const todayCard = document.getElementById('todayUsageCard');
  const todayValEl = document.getElementById('todayUsageVal');
  const todayBadge = document.getElementById('todayStatusBadge');
  const todaySubtext = document.getElementById('todaySubtext');

  const todayUsage = Number(summary.today_usage) || 0;
  if (todayValEl) todayValEl.textContent = formatCredits(todayUsage);

  if (todayUsage > 0) {
    if (todayCard) todayCard.classList.add('has-usage');
    if (todayBadge) {
      todayBadge.className = 'badge badge-success';
      todayBadge.textContent = 'Active Today';
    }
    if (todaySubtext) {
      todaySubtext.textContent = `+${formatCredits(todayUsage)} credits consumed today`;
    }
  } else {
    if (todayCard) todayCard.classList.remove('has-usage');
    if (todayBadge) {
      todayBadge.className = 'badge badge-neutral';
      todayBadge.textContent = 'No usage today';
    }
    if (todaySubtext) {
      todaySubtext.textContent = summary.current_date ? `Date: ${summary.current_date}` : 'No usage recorded today';
    }
  }

  // Pacing calculations
  const pacing = calculatePacing(summary.monthly_total, summary.today_day, summary.days_in_month, summary.quota);
  const targetPaceEl = document.getElementById('dailyTargetPace');
  const actualPaceEl = document.querySelector('#actualDailyPace span');
  const pacingBadgeEl = document.getElementById('pacingBadge');
  const daysRemEl = document.getElementById('daysRemainingText');

  if (targetPaceEl) targetPaceEl.textContent = formatCredits(pacing.targetDailyAvg);
  if (actualPaceEl) actualPaceEl.textContent = formatCredits(pacing.actualDailyAvg);
  if (pacingBadgeEl) {
    pacingBadgeEl.textContent = pacing.status;
    pacingBadgeEl.className = `badge ${pacing.badgeClass}`;
  }
  if (daysRemEl) daysRemEl.textContent = `${pacing.daysRemaining} days remaining in month`;

  // Render or update chart
  renderChart(summary);

  // Render daily breakdown table
  renderDailyTable(summary);

  // Update last updated timestamp
  const lastUpdatedEl = document.getElementById('lastUpdated');
  if (lastUpdatedEl) {
    const now = new Date();
    lastUpdatedEl.textContent = `Updated: ${now.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' })}`;
  }
}

function renderChart(summary) {
  const canvas = document.getElementById('usageChart');
  if (!canvas || typeof Chart === 'undefined') return;

  const config = buildChartConfig(summary);

  if (chartInstance) {
    chartInstance.data = config.data;
    chartInstance.options = config.options;
    chartInstance.update();
  } else {
    chartInstance = new Chart(canvas, config);
  }
}

function renderDailyTable(summary) {
  const tbody = document.getElementById('dailyTableBody');
  if (!tbody) return;

  tbody.innerHTML = '';
  const dailyList = summary.daily_usage || [];
  const quota = summary.quota || 1500;

  dailyList.forEach(item => {
    const tr = document.createElement('tr');

    if (item.is_today) {
      tr.className = 'row-today';
    } else if (item.is_future) {
      tr.className = 'row-future';
    }

    let statusBadge = '<span class="badge badge-neutral">0</span>';
    if (item.is_today) {
      statusBadge = '<span class="badge badge-today">&#x2B50; Today</span>';
    } else if (item.is_future) {
      statusBadge = '<span class="badge badge-neutral">Upcoming</span>';
    } else if (item.credits > 0) {
      statusBadge = '<span class="badge badge-success">Used</span>';
    }

    const pctOfQuota = item.cumulative_credits !== null && item.cumulative_credits !== undefined
      ? formatPercent((item.cumulative_credits / quota) * 100)
      : '--';

    const cumText = item.cumulative_credits !== null && item.cumulative_credits !== undefined
      ? formatCredits(item.cumulative_credits)
      : '--';

    const dailyText = item.is_future ? '--' : formatCredits(item.credits);

    tr.innerHTML = `
      <td>${item.day}</td>
      <td>${item.date}</td>
      <td class="text-right">${dailyText}</td>
      <td class="text-right">${cumText}</td>
      <td class="text-right">${pctOfQuota}</td>
      <td class="text-center">${statusBadge}</td>
    `;

    tbody.appendChild(tr);
  });
}

function showAlert(message) {
  const banner = document.getElementById('alertBanner');
  if (!banner) return;
  banner.textContent = message;
  banner.className = 'alert alert-danger';
  banner.classList.remove('hidden');
}

function hideAlert() {
  const banner = document.getElementById('alertBanner');
  if (banner) banner.classList.add('hidden');
}

function setLoading(isLoading) {
  const overlay = document.getElementById('loadingOverlay');
  if (overlay) {
    if (isLoading) overlay.classList.remove('hidden');
    else overlay.classList.add('hidden');
  }
}

async function loadData(year, month) {
  setLoading(true);
  hideAlert();
  try {
    const summary = await fetchUsageData(year, month);
    updateDashboardDOM(summary);
  } catch (err) {
    console.error('Failed to load dashboard data:', err);
    showAlert(`Error loading usage data: ${err.message}`);
  } finally {
    setLoading(false);
  }
}

function initDashboard() {
  // Navigation event listeners
  const prevBtn = document.getElementById('prevMonthBtn');
  const nextBtn = document.getElementById('nextMonthBtn');
  const currentBtn = document.getElementById('currentMonthBtn');
  const refreshBtn = document.getElementById('refreshBtn');

  if (prevBtn) {
    prevBtn.addEventListener('click', () => {
      if (!currentYear || !currentMonth) return;
      let y = currentYear;
      let m = currentMonth - 1;
      if (m < 1) {
        m = 12;
        y -= 1;
      }
      loadData(y, m);
    });
  }

  if (nextBtn) {
    nextBtn.addEventListener('click', () => {
      if (!currentYear || !currentMonth) return;
      let y = currentYear;
      let m = currentMonth + 1;
      if (m > 12) {
        m = 1;
        y += 1;
      }
      loadData(y, m);
    });
  }

  if (currentBtn) {
    currentBtn.addEventListener('click', () => {
      loadData();
    });
  }

  if (refreshBtn) {
    refreshBtn.addEventListener('click', () => {
      loadData(currentYear, currentMonth);
    });
  }

  // Initial load
  loadData();

  // Auto-refresh every 60 seconds
  if (refreshIntervalTimer) clearInterval(refreshIntervalTimer);
  refreshIntervalTimer = setInterval(() => {
    loadData(currentYear, currentMonth);
  }, 60000);
}

// Run on window load if in browser
if (typeof window !== 'undefined') {
  window.addEventListener('DOMContentLoaded', initDashboard);
}

// Export for Node.js unit testing
if (typeof module !== 'undefined' && module.exports) {
  module.exports = {
    formatCredits,
    formatPercent,
    calculatePacing,
    buildChartConfig
  };
}

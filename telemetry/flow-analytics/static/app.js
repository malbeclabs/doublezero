// Global state
let startPicker, endPicker;
let chart = null;
let activeTypeahead = null;
let typeaheadDebounceTimer = null;
let currentSeriesData = null; // Store current data for chart type switching
let highlightedSeriesIndex = -1;
let keyboardActiveIndex = -1; // For keyboard navigation in typeahead

// Constants
const QUERY_HISTORY_KEY = 'flow-analytics-query-history';
const RECENT_COLUMNS_KEY = 'flow-analytics-recent-columns';
const PANEL_COLLAPSED_KEY = 'flow-analytics-panel-collapsed';
const MAX_HISTORY_ITEMS = 5;
const MAX_RECENT_COLUMNS = 5;
const LARGE_TIME_RANGE_DAYS = 7; // Warn if time range exceeds this without filters

// Color palette for chart series
const CHART_COLORS = [
    '#3b82f6', '#22c55e', '#f59e0b', '#ef4444', '#8b5cf6',
    '#06b6d4', '#ec4899', '#84cc16', '#f97316', '#6366f1',
    '#14b8a6', '#a855f7', '#eab308', '#0ea5e9', '#d946ef'
];

// Initialize on DOM ready
document.addEventListener('DOMContentLoaded', function() {
    initializeDatePickers();
    initializeChart();
    loadQueryHistory();
    restorePanelState();

    // Close typeahead on outside click
    document.addEventListener('click', function(e) {
        if (!e.target.closest('.typeahead-container')) {
            closeAllTypeaheads();
        }
    });

    // Global keyboard handler for typeahead navigation
    document.addEventListener('keydown', handleGlobalKeydown);
});

// ========================================
// PANEL COLLAPSE TOGGLE
// ========================================

// Toggle panel collapsed state
function togglePanel() {
    const mainContent = document.getElementById('main-content');
    const isCollapsed = mainContent.classList.toggle('panel-collapsed');

    // Persist to localStorage
    try {
        localStorage.setItem(PANEL_COLLAPSED_KEY, isCollapsed ? 'true' : 'false');
    } catch (e) {
        console.error('Failed to save panel state:', e);
    }

    // Resize chart after transition
    setTimeout(() => {
        if (chart) {
            chart.resize();
        }
    }, 250);
}

// Restore panel state from localStorage
function restorePanelState() {
    try {
        const isCollapsed = localStorage.getItem(PANEL_COLLAPSED_KEY) === 'true';
        if (isCollapsed) {
            const mainContent = document.getElementById('main-content');
            mainContent.classList.add('panel-collapsed');
        }
    } catch (e) {
        console.error('Failed to restore panel state:', e);
    }
}

// Initialize Flatpickr date pickers
function initializeDatePickers() {
    const commonConfig = {
        enableTime: true,
        dateFormat: 'Y-m-d H:i',
        time_24hr: true,
        theme: 'dark'
    };

    // Default to last 6 hours
    const now = new Date();
    const sixHoursAgo = new Date(now.getTime() - 6 * 60 * 60 * 1000);

    startPicker = flatpickr('#start-time', {
        ...commonConfig,
        defaultDate: sixHoursAgo
    });

    endPicker = flatpickr('#end-time', {
        ...commonConfig,
        defaultDate: now
    });
}

// Set time preset
function setTimePreset(minutes) {
    const now = new Date();
    const start = new Date(now.getTime() - minutes * 60 * 1000);
    
    startPicker.setDate(start);
    endPicker.setDate(now);
    
    // Update active state
    document.querySelectorAll('.preset-btn').forEach(btn => {
        btn.classList.remove('active');
    });
    event.target.classList.add('active');
}

// Initialize ECharts
function initializeChart() {
    const container = document.getElementById('chart-container');
    chart = echarts.init(container, 'dark');
    
    // Handle resize
    window.addEventListener('resize', function() {
        chart.resize();
    });
}

// Column change handler for filters (legacy - kept for compatibility)
function onColumnChange(selectEl, type, index) {
    const column = selectEl.value;
    const row = selectEl.closest(`.${type}-row`);
    const input = row.querySelector('.typeahead-input:not(.column-typeahead-input)');

    if (input) {
        input.value = '';
        input.dataset.column = column;

        // Clear selected values
        const container = row.querySelector('.selected-values');
        if (container) {
            container.innerHTML = '';
        }
    }
}

// ========================================
// FILTER COLUMN SEARCH (with categories)
// ========================================

// Handle filter column search input
function onFilterColumnSearch(inputEl) {
    const query = inputEl.value.toLowerCase().trim();
    const dropdown = inputEl.parentElement.querySelector('.column-dropdown');

    resetKeyboardNavigation();

    const html = buildColumnDropdownHtml(query, 'filter');
    dropdown.innerHTML = html;
    dropdown.classList.add('active');
}

// Handle filter column focus
function onFilterColumnFocus(inputEl) {
    onFilterColumnSearch(inputEl);
}

// Handle filter column blur
function onFilterColumnBlur(inputEl) {
    setTimeout(() => {
        const dropdown = inputEl.parentElement.querySelector('.column-dropdown');
        dropdown.classList.remove('active');
    }, 200);
}

// Select a filter column from dropdown
function selectFilterColumn(itemEl, columnName) {
    const container = itemEl.closest('.typeahead-container');
    const input = container.querySelector('.column-typeahead-input');
    const hiddenInput = container.querySelector('.selected-column-value');
    const dropdown = container.querySelector('.column-dropdown');
    const row = container.closest('.filter-row');

    // Set the column - show description in input, store name in hidden
    const description = getColumnDescription(columnName);
    input.value = description;
    input.dataset.columnName = columnName; // Store for reference
    hiddenInput.value = columnName;
    dropdown.classList.remove('active');

    // Track column usage
    trackColumnUsage(columnName);

    // Clear value input and set column reference
    const valueInput = row.querySelector('.filter-value .typeahead-input');
    if (valueInput) {
        valueInput.value = '';
        valueInput.dataset.column = columnName;

        // Clear selected values
        const selectedContainer = row.querySelector('.selected-values');
        if (selectedContainer) {
            selectedContainer.innerHTML = '';
        }
    }
}

// Build HTML for column dropdown with categories and recent columns
function buildColumnDropdownHtml(query, context) {
    let html = '';
    const recentColumns = getRecentColumns();

    // Helper to build a column item - shows description as primary, column name as secondary
    const buildColumnItem = (col, context) =>
        `<div class="typeahead-item" onclick="select${context === 'filter' ? 'Filter' : 'GroupBy'}Column(this, '${col.name}')" data-description="${escapeHtml(col.description)}">
            <span class="column-display-name">${escapeHtml(col.description)}</span>
            <span class="column-technical-name">${escapeHtml(col.name)}</span>
        </div>`;

    // Show recent columns if no query and we have recent columns
    if (!query && recentColumns.length > 0) {
        const recentCols = COLUMNS.filter(c =>
            c.category === 'dimension' && recentColumns.includes(c.name)
        );

        if (recentCols.length > 0) {
            // Sort by recent order
            recentCols.sort((a, b) =>
                recentColumns.indexOf(a.name) - recentColumns.indexOf(b.name)
            );

            html += '<div class="column-group-header">Recently Used</div>';
            html += recentCols.map(col => buildColumnItem(col, context)).join('');
            html += '<div class="column-group-divider"></div>';
        }
    }

    // Filter and group columns
    if (query) {
        // Search mode: show flat filtered list (search both name and description)
        const filtered = COLUMNS.filter(c =>
            c.category === 'dimension' &&
            (c.name.toLowerCase().includes(query) ||
             c.description.toLowerCase().includes(query))
        );

        if (filtered.length === 0) {
            return '<div class="typeahead-empty">No matching columns</div>';
        }

        html += filtered.map(col => buildColumnItem(col, context)).join('');
    } else {
        // No query: show grouped by category
        if (typeof COLUMN_GROUPS !== 'undefined') {
            COLUMN_GROUPS.forEach(group => {
                html += `<div class="column-group-header">${escapeHtml(group.displayName)}</div>`;
                html += group.columns.map(col => buildColumnItem(col, context)).join('');
            });
        }
    }

    return html || '<div class="typeahead-empty">No columns available</div>';
}

// Get column description by name
function getColumnDescription(columnName) {
    const col = COLUMNS.find(c => c.name === columnName);
    return col ? col.description : columnName;
}

// ========================================
// RECENTLY USED COLUMNS
// ========================================

// Track column usage
function trackColumnUsage(columnName) {
    try {
        let recent = getRecentColumns();

        // Remove if already exists (to move to front)
        recent = recent.filter(c => c !== columnName);

        // Add to front
        recent.unshift(columnName);

        // Keep only last N
        recent = recent.slice(0, MAX_RECENT_COLUMNS);

        localStorage.setItem(RECENT_COLUMNS_KEY, JSON.stringify(recent));
    } catch (e) {
        console.error('Failed to track column usage:', e);
    }
}

// Get recent columns from localStorage
function getRecentColumns() {
    try {
        const stored = localStorage.getItem(RECENT_COLUMNS_KEY);
        return stored ? JSON.parse(stored) : [];
    } catch (e) {
        return [];
    }
}

// Typeahead input handler for values
function onTypeaheadInput(inputEl) {
    clearTimeout(typeaheadDebounceTimer);

    const dropdown = inputEl.parentElement.querySelector('.typeahead-dropdown');
    const row = inputEl.closest('.filter-row');

    // Try to get column from hidden input (new) or select (legacy) or data attribute
    const hiddenColumn = row.querySelector('.selected-column-value');
    const columnSelect = row.querySelector('.column-select');
    let column = inputEl.dataset.column || '';

    if (!column && hiddenColumn && hiddenColumn.value) {
        column = hiddenColumn.value;
    } else if (!column && columnSelect && columnSelect.value) {
        column = columnSelect.value;
    }

    // Reset keyboard navigation
    resetKeyboardNavigation();

    if (!column) {
        dropdown.innerHTML = '<div class="typeahead-empty">Select a column first</div>';
        dropdown.classList.add('active');
        return;
    }

    const query = inputEl.value.trim();

    dropdown.innerHTML = '<div class="typeahead-loading">Loading...</div>';
    dropdown.classList.add('active');

    typeaheadDebounceTimer = setTimeout(() => {
        fetchTypeaheadValues(column, query, dropdown, inputEl);
    }, 200);
}

// Fetch typeahead values from server
async function fetchTypeaheadValues(column, query, dropdown, inputEl) {
    try {
        const params = new URLSearchParams({
            column: column,
            q: query,
            limit: '20'
        });
        
        const response = await fetch(`/api/typeahead?${params}`);
        const html = await response.text();
        
        dropdown.innerHTML = html;
        dropdown.classList.add('active');
        
        // Store reference for value selection
        dropdown.dataset.inputId = inputEl.dataset.filterIndex;
    } catch (error) {
        console.error('Typeahead error:', error);
        dropdown.innerHTML = '<div class="typeahead-empty">Error loading values</div>';
    }
}

// Select typeahead value
function selectTypeaheadValue(itemEl, value) {
    const dropdown = itemEl.closest('.typeahead-dropdown');
    const container = dropdown.closest('.typeahead-container');
    const input = container.querySelector('.typeahead-input');
    const filterRow = container.closest('.filter-row');
    const operatorSelect = filterRow.querySelector('.operator-select');
    const operator = operatorSelect ? operatorSelect.value : '=';
    
    // For IN/NOT IN operators, add to list; otherwise replace
    if (operator === 'IN' || operator === 'NOT IN') {
        const selectedContainer = filterRow.querySelector('.selected-values');
        
        // Check if already selected
        const existing = selectedContainer.querySelectorAll('.selected-value');
        for (const el of existing) {
            if (el.dataset.value === value) {
                return;
            }
        }
        
        // Add badge
        const badge = document.createElement('span');
        badge.className = 'selected-value';
        badge.dataset.value = value;
        badge.innerHTML = `${escapeHtml(value)}<span class="remove" onclick="removeSelectedValue(this)">×</span>`;
        selectedContainer.appendChild(badge);
        
        input.value = '';
    } else {
        input.value = value;
    }
    
    dropdown.classList.remove('active');
}

// Remove selected value badge
function removeSelectedValue(removeBtn) {
    removeBtn.parentElement.remove();
}

// Focus handler for typeahead
function onTypeaheadFocus(inputEl) {
    const row = inputEl.closest('.filter-row');
    const columnSelect = row.querySelector('.column-select');
    const column = columnSelect ? columnSelect.value : '';
    
    if (column) {
        onTypeaheadInput(inputEl);
    }
}

// Blur handler for typeahead
function onTypeaheadBlur(inputEl) {
    // Delay to allow click on dropdown items
    setTimeout(() => {
        const dropdown = inputEl.parentElement.querySelector('.typeahead-dropdown');
        dropdown.classList.remove('active');
    }, 200);
}

// Column typeahead for group by (updated to use categories)
function onColumnTypeaheadInput(inputEl) {
    const query = inputEl.value.toLowerCase().trim();
    const dropdown = inputEl.parentElement.querySelector('.typeahead-dropdown');

    // Reset keyboard navigation
    resetKeyboardNavigation();

    // Use the shared buildColumnDropdownHtml function
    const html = buildColumnDropdownHtml(query, 'groupby');
    dropdown.innerHTML = html;
    dropdown.classList.add('active');
}

function onColumnTypeaheadFocus(inputEl) {
    onColumnTypeaheadInput(inputEl);
}

function onColumnTypeaheadBlur(inputEl) {
    setTimeout(() => {
        const dropdown = inputEl.parentElement.querySelector('.typeahead-dropdown');
        dropdown.classList.remove('active');
    }, 200);
}

// Select group by column (called from buildColumnDropdownHtml)
function selectGroupByColumn(itemEl, column) {
    const container = itemEl.closest('.typeahead-container');
    const input = container.querySelector('.typeahead-input');
    const dropdown = container.querySelector('.typeahead-dropdown');

    // Show description in input, store column name in dataset
    const description = getColumnDescription(column);
    input.value = description;
    input.dataset.selectedColumn = column;
    dropdown.classList.remove('active');

    // Track column usage
    trackColumnUsage(column);
}

// Legacy function name for backwards compatibility
function selectColumnTypeahead(itemEl, column) {
    selectGroupByColumn(itemEl, column);
}

// Close all typeahead dropdowns
function closeAllTypeaheads() {
    document.querySelectorAll('.typeahead-dropdown').forEach(d => {
        d.classList.remove('active');
    });
}

// Remove filter row
function removeFilterRow(index) {
    const row = document.getElementById(`filter-row-${index}`);
    if (row) {
        row.style.opacity = '0';
        row.style.transform = 'translateX(-20px)';
        setTimeout(() => row.remove(), 150);
    }
}

// Remove group by row
function removeGroupByRow(index) {
    const row = document.getElementById(`groupby-row-${index}`);
    if (row) {
        row.style.opacity = '0';
        row.style.transform = 'translateX(-20px)';
        setTimeout(() => row.remove(), 150);
    }
}

// Collect query parameters from UI
function collectQueryParams() {
    // Time range
    const startTime = startPicker.selectedDates[0];
    const endTime = endPicker.selectedDates[0];
    
    if (!startTime || !endTime) {
        throw new Error('Please select a time range');
    }
    
    // Interval
    const interval = document.getElementById('interval').value;
    
    // Filters
    const filters = [];
    document.querySelectorAll('.filter-row').forEach(row => {
        // Try hidden input first (new searchable column), fallback to select (legacy)
        const hiddenColumn = row.querySelector('.selected-column-value');
        const columnSelect = row.querySelector('.column-select');
        const operatorSelect = row.querySelector('.operator-select');
        const input = row.querySelector('.filter-value .typeahead-input');
        const selectedValues = row.querySelectorAll('.selected-value');

        // Get column from hidden input or legacy select
        let column = '';
        if (hiddenColumn && hiddenColumn.value) {
            column = hiddenColumn.value;
        } else if (columnSelect && columnSelect.value) {
            column = columnSelect.value;
        }

        if (!column) return;

        const operator = operatorSelect ? operatorSelect.value : '=';

        let values = [];
        if (operator === 'IN' || operator === 'NOT IN') {
            selectedValues.forEach(v => values.push(v.dataset.value));
        } else if (input && input.value) {
            values.push(input.value);
        }

        if (values.length > 0) {
            filters.push({ column, operator, values });
        }
    });
    
    // Group by
    const groupBy = [];
    document.querySelectorAll('.groupby-row').forEach(row => {
        const input = row.querySelector('.typeahead-input');
        if (input && input.dataset.selectedColumn) {
            groupBy.push(input.dataset.selectedColumn);
        }
    });
    
    return {
        start_time: startTime.toISOString(),
        end_time: endTime.toISOString(),
        interval: interval,
        filters: filters,
        group_by: groupBy
    };
}

// Execute query
async function executeQuery() {
    const statusEl = document.getElementById('query-status');
    const execBtn = document.querySelector('.execute-btn');

    try {
        statusEl.className = 'query-status loading';
        statusEl.textContent = 'Executing query...';
        execBtn.disabled = true;

        const params = collectQueryParams();

        // Validate time range and show warning if needed
        const validation = validateTimeRange(params);
        showTimeRangeWarning(validation.warning);

        const response = await fetch('/api/query', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json'
            },
            body: JSON.stringify(params)
        });

        const result = await response.json();

        if (result.error) {
            throw new Error(result.error);
        }

        // Update chart
        renderChart(result.series);

        // Update generated query
        document.getElementById('generated-query').textContent = result.generated_sql;

        // Update stats
        updateStats(result);

        // Save to query history
        saveQueryToHistory(params);

        // Clear warning on success
        showTimeRangeWarning(null);

        statusEl.className = 'query-status success';
        statusEl.textContent = `Query completed in ${result.execution_ms}ms`;

    } catch (error) {
        console.error('Query error:', error);
        statusEl.className = 'query-status error';
        statusEl.textContent = `Error: ${error.message}`;
    } finally {
        execBtn.disabled = false;
    }
}

// Chart type change handler
function onChartTypeChange() {
    if (currentSeriesData) {
        renderChart(currentSeriesData);
    }
}

// Render chart with ECharts
function renderChart(series) {
    // Store for chart type switching
    currentSeriesData = series;
    
    const chartType = document.getElementById('chart-type').value;
    
    if (!series || series.length === 0) {
        chart.clear();
        const container = document.getElementById('chart-container');
        container.innerHTML = '<div class="chart-placeholder">No data returned for the selected query</div>';
        chart = echarts.init(container, 'dark');
        document.getElementById('chart-legend-container').style.display = 'none';
        return;
    }
    
    // Handle Sankey chart separately
    if (chartType === 'sankey') {
        renderSankeyChart(series);
        return;
    }
    
    // Build custom legend
    buildCustomLegend(series);
    
    // Prepare series data based on chart type
    const echartsSeries = series.map((s, i) => {
        const baseSeries = {
            name: s.label,
            data: s.data.map(d => [d.timestamp, d.value]),
            emphasis: {
                focus: 'series',
                lineStyle: { width: 4 },
                areaStyle: { opacity: 0.3 }
            }
        };
        
        switch (chartType) {
            case 'line':
                return {
                    ...baseSeries,
                    type: 'line',
                    smooth: true,
                    symbol: 'none',
                    lineStyle: { width: 2 },
                    areaStyle: null
                };
            case 'area':
                return {
                    ...baseSeries,
                    type: 'line',
                    smooth: true,
                    symbol: 'none',
                    lineStyle: { width: 2 },
                    areaStyle: { opacity: 0.15 }
                };
            case 'stacked-area':
                return {
                    ...baseSeries,
                    type: 'line',
                    smooth: true,
                    symbol: 'none',
                    stack: 'Total',
                    lineStyle: { width: 1 },
                    areaStyle: { opacity: 0.6 }
                };
            case 'stacked-bar':
                return {
                    ...baseSeries,
                    type: 'bar',
                    stack: 'Total',
                    barMaxWidth: 20
                };
            default:
                return {
                    ...baseSeries,
                    type: 'line',
                    smooth: true,
                    symbol: 'none',
                    lineStyle: { width: 2 }
                };
        }
    });
    
    // Configure chart options
    const option = {
        backgroundColor: 'transparent',
        color: CHART_COLORS,
        tooltip: {
            trigger: 'axis',
            backgroundColor: 'rgba(26, 31, 46, 0.95)',
            borderColor: '#3d4558',
            borderWidth: 1,
            padding: [12, 16],
            textStyle: {
                color: '#e4e8f1',
                fontSize: 13
            },
            axisPointer: {
                type: 'cross',
                crossStyle: {
                    color: '#5a6478'
                }
            },
            formatter: function(params) {
                const time = new Date(params[0].value[0]).toLocaleString();
                let html = `<div style="margin-bottom: 10px; font-weight: 600; font-size: 12px; color: #8b95a8;">${time}</div>`;
                
                // Sort by value descending
                const sorted = [...params].sort((a, b) => b.value[1] - a.value[1]);
                
                sorted.forEach((p, idx) => {
                    const value = formatBitsPerSecond(p.value[1]);
                    const isHighlighted = highlightedSeriesIndex === p.seriesIndex;
                    const fontWeight = isHighlighted ? 'bold' : 'normal';
                    const bgColor = isHighlighted ? 'rgba(59, 130, 246, 0.15)' : 'transparent';
                    const borderLeft = isHighlighted ? '3px solid ' + p.color : '3px solid transparent';
                    
                    html += `<div style="display: flex; align-items: center; justify-content: space-between; gap: 16px; padding: 4px 8px; margin: 2px -8px; background: ${bgColor}; border-left: ${borderLeft}; font-weight: ${fontWeight};">
                        <span style="display: flex; align-items: center; gap: 8px;">
                            <span style="display: inline-block; width: 10px; height: 10px; background: ${p.color}; border-radius: 2px;"></span>
                            <span style="max-width: 200px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;">${p.seriesName}</span>
                        </span>
                        <strong style="font-family: monospace;">${value}</strong>
                    </div>`;
                });
                return html;
            }
        },
        legend: {
            show: false  // Using custom legend
        },
        grid: {
            left: 80,
            right: 30,
            top: 30,
            bottom: 20,
            containLabel: false
        },
        xAxis: {
            type: 'time',
            axisLine: {
                lineStyle: {
                    color: '#3d4558'
                }
            },
            axisLabel: {
                color: '#8b95a8',
                formatter: function(value) {
                    const date = new Date(value);
                    return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
                }
            },
            splitLine: {
                show: true,
                lineStyle: {
                    color: '#242938'
                }
            }
        },
        yAxis: {
            type: 'value',
            axisLine: {
                show: true,
                lineStyle: {
                    color: '#3d4558'
                }
            },
            axisLabel: {
                color: '#8b95a8',
                formatter: function(value) {
                    return formatBitsPerSecond(value);
                },
                margin: 12
            },
            splitLine: {
                lineStyle: {
                    color: '#242938'
                }
            }
        },
        series: echartsSeries
    };
    
    chart.setOption(option, true);
    
    // Track mouse position to highlight series
    chart.getZr().on('mousemove', function(params) {
        const pointInPixel = [params.offsetX, params.offsetY];
        if (chart.containPixel('grid', pointInPixel)) {
            const pointInGrid = chart.convertFromPixel('grid', pointInPixel);
            // Find closest series
            let minDist = Infinity;
            let closestIdx = -1;
            
            series.forEach((s, idx) => {
                s.data.forEach(d => {
                    const screenPoint = chart.convertToPixel('grid', [d.timestamp, d.value]);
                    if (screenPoint) {
                        const dist = Math.abs(screenPoint[1] - params.offsetY);
                        if (dist < minDist && dist < 30) {
                            minDist = dist;
                            closestIdx = idx;
                        }
                    }
                });
            });
            
            if (closestIdx !== highlightedSeriesIndex) {
                highlightedSeriesIndex = closestIdx;
                updateLegendHighlight(closestIdx);
            }
        }
    });
    
    chart.getZr().on('mouseout', function() {
        highlightedSeriesIndex = -1;
        updateLegendHighlight(-1);
    });
}

// Render Sankey diagram
function renderSankeyChart(series) {
    // Build nodes and links from series data
    const nodes = new Map();
    const links = new Map();
    
    series.forEach(s => {
        // Parse label to extract source and destination
        const parts = s.label.split(', ');
        let source = 'Source';
        let target = 'Target';
        
        parts.forEach(p => {
            const [key, val] = p.split('=');
            if (key && val) {
                if (key.startsWith('src')) {
                    source = val;
                } else if (key.startsWith('dst')) {
                    target = val;
                }
            }
        });
        
        // Calculate total value
        const totalValue = s.data.reduce((sum, d) => sum + d.value, 0) / s.data.length;
        
        nodes.set(source, { name: source });
        nodes.set(target, { name: target });
        
        const linkKey = `${source}->${target}`;
        if (links.has(linkKey)) {
            links.get(linkKey).value += totalValue;
        } else {
            links.set(linkKey, { source, target, value: totalValue });
        }
    });
    
    const option = {
        backgroundColor: 'transparent',
        tooltip: {
            trigger: 'item',
            triggerOn: 'mousemove',
            formatter: function(params) {
                if (params.dataType === 'edge') {
                    return `${params.data.source} → ${params.data.target}<br/>Avg: ${formatBitsPerSecond(params.data.value)}`;
                }
                return params.name;
            }
        },
        series: [{
            type: 'sankey',
            layout: 'none',
            emphasis: {
                focus: 'adjacency'
            },
            nodeAlign: 'left',
            data: Array.from(nodes.values()),
            links: Array.from(links.values()),
            lineStyle: {
                color: 'gradient',
                curveness: 0.5
            },
            itemStyle: {
                borderWidth: 1,
                borderColor: '#1a1f2e'
            },
            label: {
                color: '#e4e8f1',
                fontSize: 12
            }
        }]
    };
    
    chart.setOption(option, true);
    
    // Hide legend for sankey
    document.getElementById('chart-legend-container').style.display = 'none';
}

// Build custom legend below chart
function buildCustomLegend(series) {
    const container = document.getElementById('chart-legend-container');
    const grid = document.getElementById('chart-legend-grid');
    
    container.style.display = 'block';
    grid.innerHTML = '';
    
    series.forEach((s, i) => {
        const item = document.createElement('div');
        item.className = 'chart-legend-item';
        item.dataset.index = i;
        item.innerHTML = `
            <span class="color-dot" style="background: ${CHART_COLORS[i % CHART_COLORS.length]}"></span>
            <span class="label-text">${escapeHtml(s.label)}</span>
        `;
        
        // Click to toggle series visibility
        item.addEventListener('click', () => {
            chart.dispatchAction({
                type: 'legendToggleSelect',
                name: s.label
            });
            item.style.opacity = item.style.opacity === '0.4' ? '1' : '0.4';
        });
        
        // Hover to highlight
        item.addEventListener('mouseenter', () => {
            highlightedSeriesIndex = i;
            updateLegendHighlight(i);
            chart.dispatchAction({
                type: 'highlight',
                seriesIndex: i
            });
        });
        
        item.addEventListener('mouseleave', () => {
            highlightedSeriesIndex = -1;
            updateLegendHighlight(-1);
            chart.dispatchAction({
                type: 'downplay',
                seriesIndex: i
            });
        });
        
        grid.appendChild(item);
    });
}

// Update legend highlight state
function updateLegendHighlight(activeIndex) {
    const items = document.querySelectorAll('.chart-legend-item');
    items.forEach((item, i) => {
        if (activeIndex === -1) {
            item.classList.remove('highlighted');
        } else if (i === activeIndex) {
            item.classList.add('highlighted');
        } else {
            item.classList.remove('highlighted');
        }
    });
}

// Format bits per second with appropriate unit
function formatBitsPerSecond(bps) {
    if (bps === 0) return '0 bps';
    
    const units = ['bps', 'Kbps', 'Mbps', 'Gbps', 'Tbps'];
    const k = 1000;
    const i = Math.floor(Math.log(Math.abs(bps)) / Math.log(k));
    const index = Math.min(i, units.length - 1);
    
    return (bps / Math.pow(k, index)).toFixed(2) + ' ' + units[index];
}

// Update query stats display
function updateStats(result) {
    const statsEl = document.getElementById('query-stats');
    const seriesCount = result.series ? result.series.length : 0;
    const pointCount = result.series ? result.series.reduce((sum, s) => sum + s.data.length, 0) : 0;
    
    statsEl.innerHTML = `
        <div class="stat-item">
            <span>Series:</span>
            <span class="stat-value">${seriesCount}</span>
        </div>
        <div class="stat-item">
            <span>Data Points:</span>
            <span class="stat-value">${pointCount.toLocaleString()}</span>
        </div>
        <div class="stat-item">
            <span>Execution Time:</span>
            <span class="stat-value">${result.execution_ms}ms</span>
        </div>
    `;
}

// Copy query to clipboard
async function copyQuery() {
    const queryText = document.getElementById('generated-query').textContent;
    try {
        await navigator.clipboard.writeText(queryText);
        
        const copyBtn = document.querySelector('.copy-btn');
        const originalText = copyBtn.textContent;
        copyBtn.textContent = '✓ Copied!';
        setTimeout(() => {
            copyBtn.textContent = originalText;
        }, 2000);
    } catch (error) {
        console.error('Copy failed:', error);
    }
}

// ========================================
// CLEAR ALL FUNCTIONS
// ========================================

// Clear all filter rows
function clearAllFilters() {
    const container = document.getElementById('filters-container');
    const rows = container.querySelectorAll('.filter-row');

    rows.forEach((row, index) => {
        row.style.opacity = '0';
        row.style.transform = 'translateX(-20px)';
        setTimeout(() => row.remove(), 150);
    });
}

// Clear all group by rows
function clearAllGroupBys() {
    const container = document.getElementById('groupby-container');
    const rows = container.querySelectorAll('.groupby-row');

    rows.forEach((row, index) => {
        row.style.opacity = '0';
        row.style.transform = 'translateX(-20px)';
        setTimeout(() => row.remove(), 150);
    });
}

// ========================================
// QUERY HISTORY FUNCTIONS
// ========================================

// Load query history from localStorage
function loadQueryHistory() {
    const history = getQueryHistory();
    renderQueryHistory(history);
}

// Get query history from localStorage
function getQueryHistory() {
    try {
        const stored = localStorage.getItem(QUERY_HISTORY_KEY);
        return stored ? JSON.parse(stored) : [];
    } catch (e) {
        console.error('Failed to load query history:', e);
        return [];
    }
}

// Save query to history
function saveQueryToHistory(params) {
    try {
        let history = getQueryHistory();

        // Create a label for the query
        const label = generateQueryLabel(params);

        // Add new entry at the beginning
        history.unshift({
            params: params,
            label: label,
            timestamp: new Date().toISOString()
        });

        // Keep only the last N items
        history = history.slice(0, MAX_HISTORY_ITEMS);

        localStorage.setItem(QUERY_HISTORY_KEY, JSON.stringify(history));
        renderQueryHistory(history);
    } catch (e) {
        console.error('Failed to save query history:', e);
    }
}

// Generate a label for a query
function generateQueryLabel(params) {
    const parts = [];

    // Add time range
    const start = new Date(params.start_time);
    const end = new Date(params.end_time);
    const durationMs = end - start;
    const durationHours = Math.round(durationMs / (1000 * 60 * 60));

    if (durationHours < 24) {
        parts.push(`${durationHours}h`);
    } else {
        parts.push(`${Math.round(durationHours / 24)}d`);
    }

    // Add filter count
    if (params.filters && params.filters.length > 0) {
        parts.push(`${params.filters.length} filter${params.filters.length > 1 ? 's' : ''}`);
    }

    // Add group by columns
    if (params.group_by && params.group_by.length > 0) {
        parts.push(`by ${params.group_by.join(', ')}`);
    }

    return parts.join(' | ') || 'All data';
}

// Render query history items
function renderQueryHistory(history) {
    const section = document.getElementById('query-history-section');
    const container = document.getElementById('query-history-container');

    if (!history || history.length === 0) {
        section.style.display = 'none';
        return;
    }

    section.style.display = 'block';
    container.innerHTML = history.map((item, index) => {
        const time = new Date(item.timestamp);
        const timeStr = time.toLocaleString([], {
            month: 'short',
            day: 'numeric',
            hour: '2-digit',
            minute: '2-digit'
        });

        return `
            <div class="query-history-item" onclick="loadQueryFromHistory(${index})" title="Click to load this query">
                <span class="history-label">${escapeHtml(item.label)}</span>
                <span class="history-time">${timeStr}</span>
            </div>
        `;
    }).join('');
}

// Load a query from history
function loadQueryFromHistory(index) {
    const history = getQueryHistory();
    if (index < 0 || index >= history.length) return;

    const item = history[index];
    const params = item.params;

    // Set time range
    if (params.start_time) {
        startPicker.setDate(new Date(params.start_time));
    }
    if (params.end_time) {
        endPicker.setDate(new Date(params.end_time));
    }

    // Set interval
    if (params.interval) {
        document.getElementById('interval').value = params.interval;
    }

    // Clear existing filters and group bys
    clearAllFilters();
    clearAllGroupBys();

    // Show status
    const statusEl = document.getElementById('query-status');
    statusEl.className = 'query-status';
    statusEl.textContent = 'Query loaded from history. Click Execute to run.';
}

// Clear query history
function clearQueryHistory() {
    try {
        localStorage.removeItem(QUERY_HISTORY_KEY);
        renderQueryHistory([]);
    } catch (e) {
        console.error('Failed to clear query history:', e);
    }
}

// ========================================
// TIME RANGE VALIDATION
// ========================================

// Validate time range and show warning if needed
function validateTimeRange(params) {
    const start = new Date(params.start_time);
    const end = new Date(params.end_time);
    const durationMs = end - start;
    const durationDays = durationMs / (1000 * 60 * 60 * 24);

    // Check if time range is large and no filters
    const hasFilters = params.filters && params.filters.length > 0;

    if (durationDays > LARGE_TIME_RANGE_DAYS && !hasFilters) {
        return {
            valid: true, // Still allow execution
            warning: `Large time range (${Math.round(durationDays)} days) without filters may be slow.`
        };
    }

    return { valid: true, warning: null };
}

// Show or hide time range warning
function showTimeRangeWarning(message) {
    let warning = document.querySelector('.time-range-warning');

    if (message) {
        if (!warning) {
            warning = document.createElement('div');
            warning.className = 'time-range-warning';
            const executeSection = document.querySelector('.execute-section');
            executeSection.parentNode.insertBefore(warning, executeSection);
        }
        warning.innerHTML = `<span class="warning-icon">⚠</span> ${escapeHtml(message)}`;
    } else if (warning) {
        warning.remove();
    }
}

// ========================================
// CHART DOWNLOAD
// ========================================

// Download chart as PNG
function downloadChart() {
    if (!chart || !currentSeriesData) {
        alert('No chart data to download. Execute a query first.');
        return;
    }

    try {
        const url = chart.getDataURL({
            type: 'png',
            pixelRatio: 2,
            backgroundColor: '#0f1419'
        });

        // Create download link
        const link = document.createElement('a');
        link.href = url;
        link.download = `flow-analytics-${new Date().toISOString().slice(0, 10)}.png`;
        document.body.appendChild(link);
        link.click();
        document.body.removeChild(link);
    } catch (error) {
        console.error('Failed to download chart:', error);
        alert('Failed to download chart.');
    }
}

// ========================================
// KEYBOARD NAVIGATION
// ========================================

// Global keyboard handler
function handleGlobalKeydown(e) {
    // Find active typeahead dropdown
    const activeDropdown = document.querySelector('.typeahead-dropdown.active');
    if (!activeDropdown) {
        keyboardActiveIndex = -1;
        return;
    }

    const items = activeDropdown.querySelectorAll('.typeahead-item');
    if (items.length === 0) return;

    switch (e.key) {
        case 'ArrowDown':
            e.preventDefault();
            keyboardActiveIndex = Math.min(keyboardActiveIndex + 1, items.length - 1);
            updateKeyboardActiveItem(items);
            break;

        case 'ArrowUp':
            e.preventDefault();
            keyboardActiveIndex = Math.max(keyboardActiveIndex - 1, 0);
            updateKeyboardActiveItem(items);
            break;

        case 'Enter':
            if (keyboardActiveIndex >= 0 && keyboardActiveIndex < items.length) {
                e.preventDefault();
                const item = items[keyboardActiveIndex];
                // Trigger click - works for both onclick attribute and event listeners
                item.click();
                keyboardActiveIndex = -1;
            }
            break;

        case 'Escape':
            e.preventDefault();
            closeAllTypeaheads();
            keyboardActiveIndex = -1;
            break;
    }
}

// Update keyboard active item styling
function updateKeyboardActiveItem(items) {
    items.forEach((item, index) => {
        if (index === keyboardActiveIndex) {
            item.classList.add('keyboard-active');
            // Scroll into view if needed
            item.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
        } else {
            item.classList.remove('keyboard-active');
        }
    });
}

// Reset keyboard index when typeahead opens
function resetKeyboardNavigation() {
    keyboardActiveIndex = -1;
    document.querySelectorAll('.typeahead-item.keyboard-active').forEach(item => {
        item.classList.remove('keyboard-active');
    });
}

// Utility: Escape HTML
function escapeHtml(text) {
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}

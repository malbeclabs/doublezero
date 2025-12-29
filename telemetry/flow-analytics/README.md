# Network Flow Analytics

A web-based UI for querying and visualizing network flow data stored in ClickHouse. Built with Go, htmx, and ECharts.

![Flow Analytics UI](docs/screenshot.png)

## Features

- **Dynamic Time Range Selection**: Quick presets (15m, 1h, 6h, 24h, 7d) and custom date/time pickers
- **Smart Typeahead Filters**: Search and select values directly from your ClickHouse data
- **Flexible Group By**: Aggregate by any dimension column with typeahead column search
- **Real-time Visualization**: Interactive time series charts with bits/second metrics
- **Query Inspector**: View the generated ClickHouse SQL for learning and debugging
- **Responsive Design**: Works on desktop and tablet devices

## Architecture

```
┌─────────────────┐     ┌─────────────────┐     ┌─────────────────┐
│                 │     │                 │     │                 │
│   Browser       │────▶│   Go Server     │────▶│   ClickHouse    │
│   (htmx +       │     │   (HTTP API)    │     │   (Flow Data)   │
│    ECharts)     │◀────│                 │◀────│                 │
│                 │     │                 │     │                 │
└─────────────────┘     └─────────────────┘     └─────────────────┘
```

### Technology Stack

| Component | Technology | Purpose |
|-----------|------------|---------|
| Backend | Go 1.21+ | HTTP server, query building, ClickHouse client |
| Frontend Interactivity | htmx | Dynamic partial page updates, form handling |
| Charting | ECharts 5 | Time series visualization |
| Date Picking | Flatpickr | DateTime selection |
| Database | ClickHouse | Flow data storage and analytics |
| Containerization | Docker | Deployment and local development |

## Quick Start

### Using Docker Compose (Recommended)

1. Clone the repository:
   ```bash
   git clone <repository-url>
   cd flow-analytics
   ```

2. Start the stack:
   ```bash
   docker-compose up -d
   ```

3. Open http://localhost:8080 in your browser

The stack includes:
- **Flow Analytics UI** on port 8080
- **ClickHouse** on ports 9000 (native) and 8123 (HTTP)
- Sample data automatically loaded

### Manual Setup

1. Install dependencies:
   ```bash
   go mod download
   ```

2. Set environment variables:
   ```bash
   export CLICKHOUSE_ADDR=localhost:9000
   export FLOWS_TABLE=default.flows_integration
   export PORT=8080
   ```

3. Run the server:
   ```bash
   go run main.go
   ```

## Configuration

| Environment Variable | Default | Description |
|---------------------|---------|-------------|
| `PORT` | 8080 | HTTP server port |
| `CLICKHOUSE_ADDR` | localhost:9000 | ClickHouse address (host:port) |
| `CLICKHOUSE_SECURE` | false | Use TLS (set to `true` for ClickHouse Cloud) |
| `CLICKHOUSE_USER` | default | ClickHouse username |
| `CLICKHOUSE_PASS` | (empty) | ClickHouse password |
| `CLICKHOUSE_DATABASE` | default | ClickHouse database |
| `FLOWS_TABLE` | default.flows_testnet | Table name for flow data |

### Connecting to ClickHouse Cloud

1. Copy the example environment file:
   ```bash
   cp .env.example .env
   ```

2. Update with your ClickHouse Cloud credentials:
   ```bash
   CLICKHOUSE_ADDR=your-instance.clickhouse.cloud:8443
   CLICKHOUSE_SECURE=true
   CLICKHOUSE_USER=default
   CLICKHOUSE_PASS=your-password
   ```

3. Run the application:
   ```bash
   source .env && go run main.go
   ```

## Usage Guide

### 1. Select Time Range

Choose from quick presets or use the datetime pickers:
- **15m**: Last 15 minutes
- **1h**: Last hour
- **6h**: Last 6 hours (default)
- **24h**: Last day
- **7d**: Last week

The interval is automatically calculated based on time range, or you can set it manually.

### 2. Add Filters

Click **+ Add Filter** to filter your data:

1. Select a column from the dropdown
2. Choose an operator:
   - `=` : Exact match
   - `≠` : Not equal
   - `IN` : Match any of multiple values
   - `NOT IN` : Exclude multiple values
   - `LIKE` : Pattern matching
3. Type to search for values (fetched from ClickHouse)
4. Select one or more values

### 3. Add Group By

Click **+ Add Group By** to aggregate data:

1. Type to search for columns
2. Select a dimension column
3. Add multiple group by columns for nested aggregation

### 4. Execute Query

Click **Execute Query** to:
- Fetch time series data from ClickHouse
- Render an interactive chart
- Display the generated SQL query

### Understanding the Chart

- **Y-axis**: Bits per second (auto-scaled to bps/Kbps/Mbps/Gbps)
- **X-axis**: Time buckets based on selected interval
- **Series**: One line per unique combination of group by values
- **Tooltip**: Hover for detailed values

## Query Building

The application builds ClickHouse queries with these components:

```sql
SELECT 
    toStartOfInterval(time_received_ns, INTERVAL 1 minute) as time_bucket,
    src_as,
    dst_location,
    sum(bytes * sampling_rate) * 8 / 60 as bps
FROM default.flows_integration
WHERE 
    time_received_ns >= '2024-01-01 00:00:00'
    AND time_received_ns < '2024-01-01 06:00:00'
    AND proto = 'TCP'
GROUP BY time_bucket, src_as, dst_location
ORDER BY time_bucket ASC
```

Key calculations:
- **Bits per second**: `sum(bytes * sampling_rate) * 8 / interval_seconds`
- Sampling rate correction is applied automatically

## API Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/` | GET | Main UI |
| `/api/typeahead` | GET | Fetch column values for autocomplete |
| `/api/query` | POST | Execute flow query and return time series |
| `/api/columns` | GET | Get column metadata |
| `/api/filter/add` | GET | Get filter row HTML partial |
| `/api/groupby/add` | GET | Get group by row HTML partial |

### Typeahead API

```bash
curl "http://localhost:8080/api/typeahead?column=src_location&q=NY&limit=10"
```

### Query API

```bash
curl -X POST http://localhost:8080/api/query \
  -H "Content-Type: application/json" \
  -d '{
    "start_time": "2024-01-01T00:00:00Z",
    "end_time": "2024-01-01T06:00:00Z",
    "filters": [
      {"column": "proto", "operator": "=", "values": ["TCP"]}
    ],
    "group_by": ["src_as", "dst_location"],
    "interval": "5 minute"
  }'
```

## Project Structure

```
flow-analytics/
├── main.go                 # Application entry point & handlers
├── go.mod                  # Go module definition
├── Dockerfile              # Container build
├── docker-compose.yml      # Development stack
├── templates/
│   ├── index.html          # Main page template
│   ├── filter-row.html     # Filter row partial
│   └── groupby-row.html    # Group by row partial
├── static/
│   ├── styles.css          # Application styles
│   └── app.js              # Frontend JavaScript
└── init-db/
    └── 01_create_tables.sql # ClickHouse schema & sample data
```

## Extending

### Adding New Columns

1. Update `getColumnDefinitions()` in `main.go`
2. Columns with `category: "dimension"` appear in filters and group by
3. Columns with `category: "metric"` are used for aggregation

### Custom Metrics

Modify the query builder in `executeFlowQuery()` to add:
- Packets per second
- Flow counts
- Custom calculations

### Authentication

Add authentication middleware:

```go
func authMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // Add your auth logic here
        next.ServeHTTP(w, r)
    })
}
```

## Troubleshooting

### ClickHouse Connection Failed

1. Verify ClickHouse is running: `clickhouse-client --query "SELECT 1"`
2. Check address configuration: `CLICKHOUSE_ADDR`
3. Ensure table exists with correct name

### No Data in Chart

1. Verify time range contains data
2. Check filters aren't too restrictive
3. View generated SQL in Query section

### Slow Queries

1. Add appropriate ClickHouse indices
2. Reduce time range
3. Add filters to limit data scanned

## License

MIT License - See LICENSE file for details

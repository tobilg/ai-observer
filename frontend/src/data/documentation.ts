export interface DocSubsection {
  id: string
  title: string
}

export interface DocTableRow {
  cells: string[]
}

export interface DocTable {
  headers: string[]
  rows: DocTableRow[]
}

export interface DocContent {
  type: 'paragraph' | 'heading' | 'list' | 'note' | 'table' | 'code'
  /** Text of a paragraph, heading or note; for code, an optional label above it */
  text?: string
  items?: string[]
  level?: 2 | 3
  table?: DocTable
  /** Commands or configuration shown as written */
  code?: string
}

export interface DocSection {
  id: string
  title: string
  subsections?: DocSubsection[]
  content: DocContent[]
}

export const DOCUMENTATION_SECTIONS: DocSection[] = [
  {
    id: 'overview',
    title: 'Overview',
    content: [
      {
        type: 'paragraph',
        text: 'AI Observer is a unified observability dashboard for monitoring AI coding assistants. It collects and displays telemetry data from Claude Code, Gemini CLI, OpenAI Codex CLI, GitHub Copilot, and OpenCode, giving you real-time visibility into token usage, costs, API performance, and session activity.',
      },
      {
        type: 'heading',
        level: 3,
        text: 'Navigation',
      },
      {
        type: 'paragraph',
        text: 'The sidebar on the left provides access to all main sections:',
      },
      {
        type: 'list',
        items: [
          'Dashboards - Customizable overview with widgets showing key metrics',
          'Metrics - Time-series charts for detailed metric analysis, and the data points behind them',
          'Logs - Searchable log records with severity filtering',
          'Traces - Distributed trace visualization with waterfall view',
          'Sessions - Conversations of your AI tools and their transcripts',
          'Status - Version, endpoints, database and settings of the running server',
          'Documentation - This help page',
        ],
      },
      {
        type: 'paragraph',
        text: 'Pages keep their filters and time range in the address, so a reload or a shared link shows the same view. The current page is highlighted in the sidebar; the server version is shown at its bottom.',
      },
      {
        type: 'heading',
        level: 3,
        text: 'Real-time Updates',
      },
      {
        type: 'paragraph',
        text: 'AI Observer receives telemetry via WebSocket, so data updates automatically as your AI tools send metrics, logs, and traces. The connection status indicator in the header shows whether live updates are active.',
      },
    ],
  },
  {
    id: 'dashboard',
    title: 'Dashboard',
    subsections: [
      { id: 'dashboard-creating', title: 'Creating Dashboards' },
      { id: 'dashboard-managing', title: 'Managing Dashboards' },
      { id: 'dashboard-widgets', title: 'Widget Types' },
      { id: 'dashboard-editing', title: 'Edit Mode' },
      { id: 'dashboard-timeframe', title: 'Time Selection' },
      { id: 'dashboard-export', title: 'Export & Import' },
    ],
    content: [
      {
        type: 'paragraph',
        text: 'Dashboards provide a customizable overview of your AI tool telemetry. You can create multiple dashboards with different widget configurations to monitor various aspects of your AI coding assistants.',
      },
      {
        type: 'heading',
        level: 3,
        text: 'Creating Dashboards',
      },
      {
        type: 'paragraph',
        text: 'The server creates the Default Dashboard, with counts of traces, metrics, logs and the error rate, the active services and the recent traces, the first time it is opened. To create another dashboard, click "New Dashboard" in the sidebar under the Dashboards section, enter a name and optionally a description, and click Create. The new dashboard starts empty: click Edit to add widgets.',
      },
      {
        type: 'heading',
        level: 3,
        text: 'Managing Dashboards',
      },
      {
        type: 'paragraph',
        text: 'Each dashboard in the sidebar has a context menu (three dots icon) with the following options:',
      },
      {
        type: 'list',
        items: [
          'Rename - Change the dashboard name',
          'Set as Default - Mark this dashboard as the default, shown when you visit the root URL',
          'Delete - Remove the dashboard permanently (not available for the default dashboard)',
        ],
      },
      {
        type: 'paragraph',
        text: 'The default dashboard is marked with a star icon in the sidebar. A link to a dashboard that no longer exists shows "Dashboard not found" with a link to the default dashboard.',
      },
      {
        type: 'heading',
        level: 3,
        text: 'Widget Types',
      },
      {
        type: 'paragraph',
        text: 'When adding widgets, you can choose from two categories:',
      },
      {
        type: 'note',
        text: 'Built-in Widgets',
      },
      {
        type: 'list',
        items: [
          'Total Traces - Number of traces (and their spans) in the dashboard\'s time range',
          'Metrics Count - Number of metric data points in the time range',
          'Log Count - Number of log records in the time range',
          'Error Rate - Percentage of spans with error status in the time range',
          'Active Services - All services that have sent telemetry',
          'Recent Traces - The newest traces in the time range; click one to open it (Cmd/Ctrl-click opens it in a new tab)',
        ],
      },
      {
        type: 'note',
        text: 'Metric Widgets',
      },
      {
        type: 'list',
        items: [
          'Metric Value - Display a single metric value (e.g., total tokens, session count)',
          'Metric Chart - Time-series bar chart with optional breakdown by attribute',
        ],
      },
      {
        type: 'paragraph',
        text: 'Metric widgets can be configured to show data from specific services and metrics. For chart widgets, you can optionally select a breakdown attribute to see the metric split by different values (e.g., token usage by type or model), and whether the bars are stacked or grouped; value widgets show the total of one breakdown value. The breakdowns offered are the catalog\'s and the attributes the metric\'s points have.',
      },
      {
        type: 'paragraph',
        text: 'Histogram metrics (durations, sizes) have a Statistic: the average (default), sum, count, minimum, maximum or a percentile (p50, p90, p95, p99, estimated from the histogram buckets); token histograms default to the sum. Averages, extremes and percentiles do not add up, so they are never stacked. Bars follow your browser\'s time zone, so a daily bar is a local day.',
      },
      {
        type: 'paragraph',
        text: 'A widget whose data cannot be loaded says why, with a Retry button, instead of showing an empty value. Widgets keep their data while they refresh.',
      },
      {
        type: 'heading',
        level: 3,
        text: 'Edit Mode',
      },
      {
        type: 'paragraph',
        text: 'Click the "Edit" button in the dashboard toolbar to enter edit mode. In edit mode, you can:',
      },
      {
        type: 'list',
        items: [
          'Add widgets using the "Add Widget" button, or by clicking an empty cell to place it there',
          'Change a widget with its pencil button: title, service, metric, breakdown, statistic (histograms), chart mode, and size (width in columns, height in rows)',
          'Remove widgets by clicking the X button on each widget',
          'Rearrange widgets by dragging them by their handle to new positions (with the keyboard: focus the handle, press Space, move with the arrow keys, press Space again)',
          'Swap widget positions by dropping one widget onto another',
          'Insert an empty row with the "Add row" control between rows',
        ],
      },
      {
        type: 'paragraph',
        text: 'A widget keeps its position when it is resized and grows to the right and down; a size that would overlap another widget or leave the grid is refused with an explanation. Changes are shown at once and undone again if the server refuses them. Click Done to leave edit mode: rows left empty are closed. Edit mode ends when you open another dashboard, and is not available on small screens.',
      },
      {
        type: 'paragraph',
        text: 'The dashboard uses a 4-column grid layout. By default, stat widgets take 1 column, charts 2 columns, and the recent traces table all 4 columns; edit a widget to change its size.',
      },
      {
        type: 'heading',
        level: 3,
        text: 'Time Selection',
      },
      {
        type: 'paragraph',
        text: 'The dashboard toolbar includes a date range picker that controls the time period for all widgets. You can choose from preset ranges or define a custom date range. The selection is part of the page address (timeframe, or from and to), so a link to the dashboard shows the same range; the last preset range is also remembered for the next visit.',
      },
      {
        type: 'note',
        text: 'Preset Time Ranges',
      },
      {
        type: 'paragraph',
        text: 'Choose from relative time ranges that automatically update as time passes:',
      },
      {
        type: 'list',
        items: [
          'Short ranges: 1 minute, 5 minutes, 15 minutes, 30 minutes',
          'Medium ranges: 1 hour, 3 hours, 6 hours, 12 hours, 24 hours',
          'Long ranges: 3 days, 7 days, 14 days, 30 days, 45 days, 60 days, 90 days, 180 days, 1 year',
        ],
      },
      {
        type: 'note',
        text: 'Custom Date Range',
      },
      {
        type: 'paragraph',
        text: 'Select "Custom date range" from the dropdown and use the calendar to pick specific start and end dates. Custom ranges are fixed in time and do not auto-refresh like relative ranges. They are kept in the page address only: opening the page again without them returns to the last preset range. When selecting a custom range:',
      },
      {
        type: 'list',
        items: [
          'Click the date picker button in the toolbar',
          'Select "Custom date range" from the dropdown',
          'Click on the start date, then the end date in the calendar',
          'The time range is automatically set to full days (00:00 to 23:59)',
        ],
      },
      {
        type: 'paragraph',
        text: 'A notification appears when you select a custom range, reminding you that data will not auto-refresh. For ranges exceeding 180 days, a performance warning is displayed.',
      },
      {
        type: 'heading',
        level: 3,
        text: 'Export & Import',
      },
      {
        type: 'paragraph',
        text: 'Dashboards can be exported to JSON files and imported to share configurations or back up your layouts.',
      },
      {
        type: 'note',
        text: 'Exporting a Dashboard',
      },
      {
        type: 'paragraph',
        text: 'To export the current dashboard, click the "Export" button in the toolbar. A JSON file will be downloaded containing:',
      },
      {
        type: 'list',
        items: [
          'Dashboard name and description',
          'All widget configurations (type, position, size, metric settings)',
          'Schema version for compatibility checking',
        ],
      },
      {
        type: 'paragraph',
        text: 'The export file does not include IDs or timestamps, making it portable across different AI Observer instances.',
      },
      {
        type: 'note',
        text: 'Importing a Dashboard',
      },
      {
        type: 'paragraph',
        text: 'To import a dashboard, click "New Dashboard" in the sidebar and select the "Import" tab. You can import from two sources:',
      },
      {
        type: 'list',
        items: [
          'From File - Click to select a JSON file from your computer',
          'From URL - Enter a URL pointing to a dashboard JSON file and click "Fetch"',
        ],
      },
      {
        type: 'paragraph',
        text: 'The import validates the file structure before allowing you to proceed. If the file is valid, a preview shows the dashboard name, description, and widget count. Click "Import" to create the new dashboard.',
      },
    ],
  },
  {
    id: 'traces',
    title: 'Traces',
    subsections: [
      { id: 'traces-filtering', title: 'Filtering & Search' },
      { id: 'traces-waterfall', title: 'Waterfall View' },
      { id: 'traces-details', title: 'Span Details' },
    ],
    content: [
      {
        type: 'paragraph',
        text: 'The Traces page shows distributed traces from your AI coding tools. Each trace represents a series of related operations, such as an API request and its associated tool executions.',
      },
      {
        type: 'heading',
        level: 3,
        text: 'Filtering & Search',
      },
      {
        type: 'paragraph',
        text: 'Use the controls at the top of the page to filter traces:',
      },
      {
        type: 'list',
        items: [
          'Timeframe - Select a time range from 1 minute to 30 days (default: 7 days)',
          'Service - Filter by a specific service name, or show all services',
          'Search - Text in span names, errors, and attribute values (case ignored)',
          'Background requests - Also list Codex\'s internal requests outside a conversation',
        ],
      },
      {
        type: 'paragraph',
        text: 'All filters are kept in the page address, so a link or the browser\'s Back button shows the same list. When new traces matching the filters arrive, a button shows their count; click it to load them. Rows never move while you read. Each row has an Open link to the trace\'s own page and, when the trace belongs to a session, a link to its transcript.',
      },
      {
        type: 'heading',
        level: 3,
        text: 'Waterfall View',
      },
      {
        type: 'paragraph',
        text: 'Click on any trace in the list to expand it and see the waterfall visualization. The waterfall shows:',
      },
      {
        type: 'list',
        items: [
          'Hierarchical span structure with parent-child relationships',
          'Duration bars scaled to the trace timeline',
          'Color-coded status (green for OK, red for ERROR)',
          'Span names with icons indicating the span kind (CLIENT, SERVER, INTERNAL)',
        ],
      },
      {
        type: 'paragraph',
        text: 'Spans with children can be expanded or collapsed by clicking the chevron icon. This helps navigate complex traces with many nested operations.',
      },
      {
        type: 'paragraph',
        text: 'The trace page adds a time axis above the bars and a filter that matches span names, IDs and attribute keys and values among the loaded spans: matching spans are highlighted and their parents stay visible. Errors only shows failed spans, and Expand all / Collapse all open or close the tree. View logs opens the Logs page filtered to the trace, and Open session its transcript.',
      },
      {
        type: 'heading',
        level: 3,
        text: 'Span Details',
      },
      {
        type: 'paragraph',
        text: 'Click on any span in the waterfall to view its details:',
      },
      {
        type: 'list',
        items: [
          'Overview - IDs, service, kind, scope, start (with its offset from the trace start), duration and status',
          'Attributes - Key-value metadata attached to the span (e.g., model name, token counts), shown in full: long values wrap, JSON is formatted, and very long values open with Show all',
          'Resource - The attributes of the tool that sent the span',
          'Events - Timestamped events that occurred during the span execution, with their attributes',
          'Links - Other spans the span refers to',
          'Status - Error messages and status codes for failed spans',
        ],
      },
      {
        type: 'paragraph',
        text: 'Every value and ID has a copy button.',
      },
      {
        type: 'note',
        text: 'Codex traces: Codex 0.167 and later send a trace per turn (root turn/start) from codex-app-server, plus hundreds of traces an hour of internal requests outside a conversation (fs.read_file, getAuthStatus, thread/read, ...) and a trace per automatic approval review (decide_request). The trace list leaves those out; turn on Background requests to list them. The app server\'s spans for every streamed response event, session file write and file status read (receiving, handle_responses, append_items, persist_rollout_items, realtime_conversation.running_state, fs.get_metadata; about 86% of its spans) are not stored, except failed ones and parents of stored spans; AI_OBSERVER_STORE_ALL_SPANS=true stores them all. Earlier Codex CLI versions use a single trace for a long session, which AI Observer shows as one raw OTLP trace row; open the trace detail to inspect every span in it.',
      },
    ],
  },
  {
    id: 'metrics',
    title: 'Metrics',
    subsections: [
      { id: 'metrics-selection', title: 'Metric Selection' },
      { id: 'metrics-charts', title: 'Chart Visualization' },
      { id: 'metrics-breakdown', title: 'Breakdown and Statistic' },
      { id: 'metrics-points', title: 'Data Points' },
      { id: 'metrics-timerange', title: 'Time Range' },
    ],
    content: [
      {
        type: 'paragraph',
        text: 'The Metrics page displays time-series data for the metrics collected from your AI coding tools. This includes token usage, API latency, costs, and various operational metrics.',
      },
      {
        type: 'heading',
        level: 3,
        text: 'Metric Selection',
      },
      {
        type: 'paragraph',
        text: 'Use the dropdown menus to select which data to display:',
      },
      {
        type: 'list',
        items: [
          'Service - Optionally filter to a specific service',
          'Metric - Choose from available metrics, grouped by source tool:',
        ],
      },
      {
        type: 'note',
        text: 'Claude Code metrics include: Sessions, Token Usage, Cost, Lines of Code, Pull Requests, Commits, Edit Decisions, Active Time',
      },
      {
        type: 'note',
        text: 'Gemini CLI metrics include: Sessions, Token Usage, Cost, API Requests, API Latency, Tool Calls, File Operations, Agent Runs, and more',
      },
      {
        type: 'note',
        text: 'Codex metrics include: Token Usage and Cost (derived from log events, by model and client: Codex app or CLI), and Codex\'s own turn metrics (Turn Tokens, Turns, Turn Duration, Time to First Token). Select OpenAI Codex (all services) to see the metrics of the Codex app and the CLI together.',
      },
      {
        type: 'note',
        text: 'GitHub Copilot metrics include: Sessions, Token Usage, Cost, Tool Calls, Agent Turns, Edit Outcomes, Lines of Code, and Cloud PR events',
      },
      {
        type: 'note',
        text: 'OpenCode metrics include: Sessions, Token Usage, Cost, Lines of Code, Tool Duration, Cache Activity, Model Usage, and Retries',
      },
      {
        type: 'paragraph',
        text: 'When you select a metric, its metadata is displayed below the selection: the metric name, unit of measurement, and description. Service, metric, breakdown, statistic, time range and chart mode are kept in the page address, so a reload or a shared link shows the same chart.',
      },
      {
        type: 'heading',
        level: 3,
        text: 'Chart Visualization',
      },
      {
        type: 'paragraph',
        text: 'The metric data is displayed as a bar chart with time on the x-axis. Two visualization modes are available:',
      },
      {
        type: 'list',
        items: [
          'Stacked - Bars are stacked on top of each other, showing cumulative totals',
          'Grouped - Bars are placed side-by-side for direct comparison between series',
        ],
      },
      {
        type: 'paragraph',
        text: 'Use the toggle buttons above the chart to switch between modes. The legend below the chart shows all data series with their colors.',
      },
      {
        type: 'heading',
        level: 3,
        text: 'Breakdown and Statistic',
      },
      {
        type: 'paragraph',
        text: 'Choose a Breakdown to split the chart by model, repository or any other attribute the metric records: the list offers the catalog\'s breakdowns and the attribute keys of the metric\'s points. Without one, counters are split by token type. A chart shows at most 100 series.',
      },
      {
        type: 'paragraph',
        text: 'Histogram metrics (durations, sizes) show their average per bucket; the Statistic selector switches to a percentile (p50, p90, p95, p99, estimated from the histogram buckets by linear interpolation), the minimum or maximum, the sum or the count. Averages, extremes and percentiles are shown side by side, not stacked, and buckets without values are left empty instead of showing 0. Token histograms show their sum by default.',
      },
      {
        type: 'paragraph',
        text: 'Buckets follow your browser\'s time zone: a daily bar runs from local midnight to midnight, also on days when daylight saving time starts or ends.',
      },
      {
        type: 'heading',
        level: 3,
        text: 'Data Points',
      },
      {
        type: 'paragraph',
        text: 'The Data points tab next to Chart lists the stored data points of the selected metric in the time range, newest first, a page at a time: time, service, metric type (with "cumulative" or "delta" for counters), value (count and sum for histograms) and attributes. It shows what the chart is made of. Cumulative counters, such as Gemini CLI\'s, are stored as running totals; the chart shows their increase per interval.',
      },
      {
        type: 'heading',
        level: 3,
        text: 'Time Range',
      },
      {
        type: 'paragraph',
        text: 'Select a time range from the dropdown to adjust the displayed period: presets from 1 minute to 1 year, or a custom date range. The chart automatically adjusts its aggregation interval based on the selected timeframe:',
      },
      {
        type: 'list',
        items: [
          'Short ranges (1-30 minutes) - 1- to 30-second buckets',
          'Medium ranges (1-24 hours) - 1-minute to hourly buckets',
          'Long ranges (3-45 days) - 3-hour to daily buckets',
          'Very long ranges (60 days to 1 year) - 2-day to 2-week buckets',
        ],
      },
      {
        type: 'paragraph',
        text: 'The chart refreshes when new metrics arrive and as the time window moves, keeping the shown chart while it loads. The badge next to the page title says "Live" while it follows new data, and "Paused (fixed range)" for a custom date range, which shows fixed historical data. A failed request is shown in place of the chart, with a Retry button.',
      },
    ],
  },
  {
    id: 'logs',
    title: 'Logs',
    subsections: [
      { id: 'logs-filtering', title: 'Filtering' },
      { id: 'logs-details', title: 'Log Details' },
      { id: 'logs-correlation', title: 'Trace Correlation' },
    ],
    content: [
      {
        type: 'paragraph',
        text: 'The Logs page displays structured log records from your AI coding tools. Logs capture events like user prompts, API requests, tool executions, and errors.',
      },
      {
        type: 'heading',
        level: 3,
        text: 'Filtering',
      },
      {
        type: 'paragraph',
        text: 'Use the filter controls to narrow down the displayed logs:',
      },
      {
        type: 'list',
        items: [
          'Timeframe - A relative range (default: 7 days) or fixed dates',
          'Service - Filter by a specific service name',
          'Level - The levels that occur in the selected range, with their number of logs',
          'Search - Text in log messages and attribute values, such as tool output (case ignored)',
        ],
      },
      {
        type: 'paragraph',
        text: 'All filters are kept in the page address, so a link or the browser\'s Back button shows the same list.',
      },
      {
        type: 'paragraph',
        text: 'Severity levels are color-coded: ERROR and FATAL appear in red, WARN in yellow, and informational levels in neutral colors.',
      },
      {
        type: 'heading',
        level: 3,
        text: 'Log Details',
      },
      {
        type: 'paragraph',
        text: 'Each log entry shows:',
      },
      {
        type: 'list',
        items: [
          'Timestamp - When the log was recorded',
          'Severity badge - The log level',
          'Service name - Which tool generated the log',
          'Message preview - Truncated log body',
        ],
      },
      {
        type: 'paragraph',
        text: 'Click on any log entry to expand it and see the full details:',
      },
      {
        type: 'list',
        items: [
          'Full message - Complete log body text, with a copy button',
          'Attributes - Structured metadata (key-value pairs) attached to the log',
          'Resource and scope - The attributes of the tool that sent the log',
          'Session - A link to the transcript of the log\'s session',
        ],
      },
      {
        type: 'heading',
        level: 3,
        text: 'Trace Correlation',
      },
      {
        type: 'paragraph',
        text: 'Many logs are correlated with traces, allowing you to see the broader context of an operation. When a log has trace context, the expanded view shows:',
      },
      {
        type: 'list',
        items: [
          'Trace ID - A link to the trace\'s page',
          'Span ID - Identifies the specific span within the trace',
        ],
      },
      {
        type: 'paragraph',
        text: 'In the other direction, View logs on a trace\'s page lists the logs of that trace.',
      },
      {
        type: 'note',
        text: 'Real-time updates: When new logs matching the filters arrive, a button shows their count. Click it to load them; rows never move while you read.',
      },
    ],
  },
  {
    id: 'sessions',
    title: 'Sessions',
    subsections: [
      { id: 'sessions-list', title: 'Session List' },
      { id: 'sessions-search', title: 'Searching Sessions' },
      { id: 'sessions-transcript', title: 'Transcripts' },
    ],
    content: [
      {
        type: 'paragraph',
        text: 'The Sessions page lists the conversations of your AI coding tools, and each session opens as a transcript of its prompts, responses and tool calls.',
      },
      {
        type: 'heading',
        level: 3,
        text: 'Session List',
      },
      {
        type: 'paragraph',
        text: 'Sessions with activity in the selected range are listed, most recently active first. A row shows the first prompt as title, the session ID (with a copy button), tool, model, duration and message count, and where they are known the repository and branch (imported and watched Claude Code and Codex sessions) and cost and tokens (sessions whose events carry them, such as Claude Code over OTLP). The first page refreshes as sessions gain messages.',
      },
      {
        type: 'heading',
        level: 3,
        text: 'Searching Sessions',
      },
      {
        type: 'paragraph',
        text: 'Search takes words and "quoted phrases"; a session matches when one of its records in the range contains all of them. Results come newest match first and show the text around the match with the terms highlighted, the number of matching records and the time of the newest one.',
      },
      {
        type: 'heading',
        level: 3,
        text: 'Transcripts',
      },
      {
        type: 'list',
        items: [
          'Header - The first prompt, the session ID, and totals for the whole session: cost, tokens, prompts, responses, tool calls and tool errors',
          'Views - Chat or Timeline; long sessions load 500 messages at a time and render only the messages near the view',
          'Filters - Show or hide prompts, responses, tool calls and tool results, and responses without content',
          'Search - Finds messages in the whole session, also those not loaded yet; Previous and Next (Enter, Shift+Enter) move between matches and load further messages as needed',
          'Copy - Buttons on messages, tool input and output, and code blocks',
          'Live - New messages of an open session appear as they arrive',
        ],
      },
      {
        type: 'paragraph',
        text: 'A session that arrived both over OTLP and from its session file (import, watch or serve --watch) shows each message once, filled in with what the other records add (tool name, usage, duration): Claude Code messages are matched by entry, request and tool call IDs, Codex messages by tool call ID and prompts by time and text, Gemini CLI messages by prompt text and time, response token counts and tool arguments. The session list counts messages and usage the same way. Claude Code replies sent over OTLP (assistant_response, with content logging) appear with the usage of their request.',
      },
    ],
  },
  {
    id: 'status',
    title: 'Status',
    content: [
      {
        type: 'paragraph',
        text: 'The Status page (System → Status in the sidebar) shows the running server, read from GET /api/status. It refreshes when telemetry arrives.',
      },
      {
        type: 'list',
        items: [
          'Server - Version, commit, build date, mode (serve: OTLP and dashboard; watch: session files and dashboard; serve+watch: both, with the watched tools) and uptime',
          'Endpoints - The OTLP/HTTP and OTLP/gRPC endpoints to configure in your AI tools (with copy buttons), the dashboard and API address, and the MCP endpoint when enabled. In watch mode OTLP ingestion is off, and gRPC can be turned off with AI_OBSERVER_OTLP_GRPC_PORT=off, which the page says',
          'Database - Path of the DuckDB file (with a copy button), its size, the data in it and the free space inside the file, the size of its write-ahead log, growth per day and, with a size limit, the days until it is reached, and the memory limit and threads (AI_OBSERVER_DB_MEMORY_LIMIT, AI_OBSERVER_DB_THREADS). It suggests ai-observer compact when much of the file is free, and a retention or size limit when nothing bounds the database',
          'Telemetry - Stored traces, spans, logs and metric points, the oldest and newest record, and the services',
          'Settings - Retention, size limit, redaction, authentication, TLS, the MCP endpoint, the admin API, whether noise spans are stored, the log level and format and the dashboard origin, with the environment variable of each. Values of secrets are never shown: only whether a token is required',
          'Maintenance - With AI_OBSERVER_ADMIN_API=true: count and delete the telemetry of a range (all, traces, logs or metric points, optionally of one tool), run a checkpoint, and write a backup into AI_OBSERVER_BACKUP_DIR. Compaction needs the server stopped: run ai-observer compact',
          'Pricing - The price files costs are calculated with: models, when they were last updated, and overrides from AI_OBSERVER_PRICING_DIR',
          'Imported session files - Files, records, last import and partly read files per tool, for import and watch',
        ],
      },
      {
        type: 'note',
        text: 'Prices older than 7 days are marked: costs of models released since may be missing. Run ai-observer pricing update to fetch current prices into AI_OBSERVER_PRICING_DIR (see docs/pricing.md).',
      },
    ],
  },
  {
    id: 'maintenance',
    title: 'Maintenance',
    content: [
      {
        type: 'paragraph',
        text: 'AI Observer keeps all telemetry unless told otherwise. The Status page shows the database size, the free space inside the file and its growth per day, and the server logs them at startup.',
      },
      {
        type: 'list',
        items: [
          'Bounding growth - AI_OBSERVER_RETENTION_DAYS deletes telemetry older than that many days; AI_OBSERVER_MAX_DB_SIZE (such as 20GB) deletes the oldest days while the stored data exceeds the size, never the last 24 hours. Both run at startup and then hourly.',
          'Free space - Deleted data leaves free space inside the database file, which new data reuses, but the file keeps its size. Stop AI Observer and run ai-observer compact to write the database into a new file without it; the copy is checked before it replaces the database.',
          'Backups - ai-observer backup --output <file> writes a checked copy of the database; restore it by copying it over the database file with AI Observer stopped. Before migrating a database to a new version, AI Observer copies it to <database>.pre-migration-v<N> (AI_OBSERVER_SKIP_MIGRATION_BACKUP=true skips it).',
          'Exports - ai-observer export writes Parquet files with the metric baselines, dashboards, the import state of session files and a manifest, from one snapshot of the database. Records keep the paths of their session files, so an export contains local file paths. ai-observer restore loads them back, refusing to duplicate the records of the export\'s range unless --replace is given, and redacts the text again (AI_OBSERVER_REDACT).',
          'Restored sessions are not imported twice: importing or watching the same session files afterwards skips unchanged files and replaces the records of grown ones. When the files moved (another computer or home directory), pass restore --map-path OLD=NEW. Exports of earlier versions carry no paths: rebuild their sessions with import --purge.',
          'While the server runs - With AI_OBSERVER_ADMIN_API=true, the Maintenance section of the Status page deletes a range, runs a checkpoint and writes a backup (POST /api/admin/delete, /checkpoint, /backup; /export writes an export), into AI_OBSERVER_BACKUP_DIR (default: backups next to the database). Protect it with AI_OBSERVER_AUTH_TOKEN beyond localhost.',
        ],
      },
      {
        type: 'code',
        text: 'Maintenance commands (AI Observer stopped)',
        code: 'ai-observer compact --dry-run\nai-observer backup --output ./ai-observer-backup.duckdb\nai-observer restore ./export --replace',
      },
    ],
  },
  {
    id: 'import-watch',
    title: 'Local Import & Watch',
    content: [
      {
        type: 'paragraph',
        text: 'Use ai-observer import to load historical Claude Code, Codex CLI, and Gemini CLI session files, or ai-observer watch to ingest new file content as it arrives. Both modes provide transcripts and token/cost metrics. Full Claude Code imports also reconstruct selected activity metrics described in the Telemetry Reference. GitHub Copilot and OpenCode use OTLP ingestion only.',
      },
      {
        type: 'paragraph',
        text: 'watch receives no OTLP. ai-observer serve --watch [tool] [--backfill] receives OTLP and reads session files in one process; the Status page then shows the mode serve+watch.',
      },
      {
        type: 'heading',
        level: 3,
        text: 'Sessions Sent Both Ways',
      },
      {
        type: 'paragraph',
        text: 'A request recorded both over OTLP and in its session file is counted once, from OTLP: Claude Code requests are matched by their request ID, Codex responses by equal token counts of the same session within 30 seconds, Gemini CLI responses by equal token counts of the same session within an hour (neither sends a shared ID). Usage only one side recorded is kept. Transcripts show such messages once (see Sessions).',
      },
      {
        type: 'note',
        text: 'After upgrading, re-import once with --force (stop watch or serve --watch first; watch does not repair stored sessions): ai-observer import claude-code --force adds the prompts typed as plain text, which imports and watch dropped since v0.3.0; ai-observer import codex --force adds the missing Codex tool calls and removes Codex usage stored twice; ai-observer import gemini --force removes Gemini CLI usage stored twice. Claude Code usage stored twice is removed when the database is migrated.',
      },
      {
        type: 'paragraph',
        text: 'Codex sessions are imported with every tool call (commands, patches, MCP and web tools, image views, also from code mode), with input, output (at most 16 KiB, stored in the tool.output attribute; the log body is a summary line), exit code and duration. Codex compresses sessions older than 7 days to .jsonl.zst; these are imported and watched too, as the same file as the .jsonl they replaced. A plain import codex after upgrading picks up the compressed sessions but keeps the old records of the others, hence the --force above.',
      },
      {
        type: 'note',
        text: 'Large imports take time, disk space and memory: 714 Codex session files (about 4.4 GB) took 285 seconds and gave a 2.87 GB database, while DuckDB cached about 3 GB. Set AI_OBSERVER_DB_MEMORY_LIMIT=512MB for the import to keep it below 1 GB.',
      },
      {
        type: 'heading',
        level: 3,
        text: 'Repository Attribution',
      },
      {
        type: 'paragraph',
        text: 'Claude Code and Codex logs and token/cost metrics receive repository and git_branch attributes when session evidence is available. This includes Codex session-start, user/agent event, transcript, tool-call, tool-result, and reasoning logs. Repository selection uses the following order:',
      },
      {
        type: 'list',
        items: [
          'Codex repository URL recorded in session metadata.',
          'The local working tree origin remote, including Git worktrees and the original working directory recorded by Claude Code.',
          'An unambiguous repository reference matching the working directory name; an exact name match takes precedence over an ancestor directory match.',
          'The working directory name. Without a working directory, a single distinct reference can identify the repository; conflicting references leave it unset.',
        ],
      },
      {
        type: 'paragraph',
        text: 'Fallback references come from structured Claude PR links, GitHub repository/PR URLs, and literal gh pr --repo or -R arguments in user/assistant text, text tool results, and supported shell tool inputs. References to other repositories cannot override a recorded Git URL or local remote. Text references do not create PR or commit activity metrics.',
      },
      {
        type: 'paragraph',
        text: 'System/developer messages, reasoning summaries, arbitrary tool arguments, and structured Codex output objects are excluded from reference detection. Placeholder examples, lookalike GitHub hosts, and dynamically constructed repository names are ignored. Git URL credentials are omitted from stored repository identifiers.',
      },
      {
        type: 'heading',
        level: 3,
        text: 'Restarts and Existing Data',
      },
      {
        type: 'paragraph',
        text: 'Full imports can use repository evidence anywhere in a file. Watch mode uses evidence through the end of each parsed batch and retains that context across restarts. When resuming older state, starting after a full import, or starting without backfill, the watcher reads the skipped file prefix once to recover context without importing its old records again. Later references can improve subsequent batches; existing database records are not automatically updated.',
      },
      {
        type: 'note',
        text: 'Import state skips unchanged files. Re-importing a changed or forced file replaces the records stored from it, also for files imported by earlier versions, and keeps live telemetry. To replace overlapping live telemetry of the imported tools, use the import command with --purge and the intended date range.',
      },
    ],
  },
  {
    id: 'agents',
    title: 'Agent Access (MCP)',
    subsections: [
      { id: 'agents-enabling', title: 'Enabling' },
      { id: 'agents-connecting', title: 'Connecting an Agent' },
      { id: 'agents-tools', title: 'Tools' },
      { id: 'agents-search', title: 'Searching Conversations' },
      { id: 'agents-resources', title: 'Transcripts as Resources' },
      { id: 'agents-apps', title: 'Charts in MCP Apps Hosts' },
      { id: 'agents-security', title: 'Security' },
      { id: 'agents-behavior', title: 'Behavior' },
    ],
    content: [
      {
        type: 'paragraph',
        text: 'AI Observer can serve its data to AI agents over the Model Context Protocol (MCP): an agent such as Claude Code, Codex CLI, Gemini CLI, GitHub Copilot or OpenCode can then search your earlier conversations, read transcripts, and look at logs, traces, metrics, token usage and costs.',
      },
      {
        type: 'paragraph',
        text: 'The endpoint is /mcp on the API port (http://localhost:8080/mcp, Streamable HTTP). It is off by default.',
      },
      {
        type: 'heading',
        level: 3,
        text: 'Enabling',
      },
      {
        type: 'paragraph',
        text: 'Either set the environment variable or pass the flag to serve or watch:',
      },
      {
        type: 'code',
        code: 'AI_OBSERVER_MCP=true ai-observer\nai-observer serve --mcp\nai-observer watch all --mcp',
      },
      {
        type: 'paragraph',
        text: 'The server logs "MCP endpoint for agents enabled" with the URL. Without it, /mcp answers 404 with a hint how to enable it.',
      },
      {
        type: 'heading',
        level: 3,
        text: 'Connecting an Agent',
      },
      {
        type: 'paragraph',
        text: 'Replace the URL if you changed AI_OBSERVER_API_PORT or AI_OBSERVER_HOST. With AI_OBSERVER_AUTH_TOKEN set, add the token as a bearer header (shown in each example); without it, leave the header out.',
      },
      {
        type: 'code',
        text: 'Claude Code',
        code: 'claude mcp add --transport http ai-observer http://localhost:8080/mcp \\\n  --header "Authorization: Bearer $AI_OBSERVER_AUTH_TOKEN"',
      },
      {
        type: 'code',
        text: 'OpenAI Codex CLI (~/.codex/config.toml)',
        code: '[mcp_servers.ai-observer]\nurl = "http://localhost:8080/mcp"\nbearer_token_env_var = "AI_OBSERVER_AUTH_TOKEN"   # only with a token',
      },
      {
        type: 'code',
        text: 'Gemini CLI (~/.gemini/settings.json)',
        code: '{\n  "mcpServers": {\n    "ai-observer": {\n      "httpUrl": "http://localhost:8080/mcp",\n      "headers": { "Authorization": "Bearer <token>" }\n    }\n  }\n}',
      },
      {
        type: 'code',
        text: 'GitHub Copilot in VS Code (.vscode/mcp.json or the user mcp.json)',
        code: '{\n  "servers": {\n    "ai-observer": {\n      "type": "http",\n      "url": "http://localhost:8080/mcp",\n      "headers": { "Authorization": "Bearer <token>" }\n    }\n  }\n}',
      },
      {
        type: 'code',
        text: 'OpenCode (opencode.json)',
        code: '{\n  "mcp": {\n    "ai-observer": {\n      "type": "remote",\n      "url": "http://localhost:8080/mcp",\n      "headers": { "Authorization": "Bearer <token>" }\n    }\n  }\n}',
      },
      {
        type: 'paragraph',
        text: 'Then ask, for example: "What did I try last week to fix the flaky parser test?", "What did my Claude Code usage cost per repository this month?", or "Show the errors Codex logged today."',
      },
      {
        type: 'heading',
        level: 3,
        text: 'Tools',
      },
      {
        type: 'paragraph',
        text: 'All tools are read-only. Time arguments (from, to) accept an RFC 3339 time (2026-11-05T10:00:00Z), a date (2026-11-05, UTC; for to, the end of that day) or an age relative to now (30m, 24h, 7d, 2w). tool accepts claude-code (or claude), codex, gemini, opencode, copilot-chat, github-copilot, or an OTLP service name, which selects only that service (codex covers Codex\'s three: codex_cli_rs, codex-app-server and codex-exec-server). Lists are paged: pass a result\'s next_cursor as cursor with the same arguments.',
      },
      {
        type: 'table',
        table: {
          headers: ['Tool', 'What it returns', 'Main arguments (defaults)'],
          rows: [
            { cells: ['search_conversations', 'Sessions with a prompt, reply or tool call that contains all words of the query, in any order, newest match first, with a snippet around the match, and each session\'s title, repository, branch, cost and tokens where known', 'query, tool, from (30 days before to), to (now), limit (10, max 50)'] },
            { cells: ['list_sessions', 'Sessions with activity in a range, most recently active first: tool, model, start, last activity, message count (as in the transcript), title, repository, branch, cost and tokens where known', 'tool, from (7 days), to, limit (20, max 100), cursor'] },
            { cells: ['get_transcript', 'A session\'s messages in order: role, content, model, tool name, input and output, tokens, cost; the session\'s title and totals over all of its messages. Messages recorded over OTLP and in the session file appear once. With search, only the matching messages, each with its position, which can be passed as cursor to read around it', 'session_id, roles (all), search, limit (50, max 200), cursor, max_chars per text (2000, max 20000)'] },
            { cells: ['usage_summary', 'Tokens by type and cost in USD; unpriced_models lists models with tokens but no price (no cost)', 'from (7 days), to, group_by: tool, model, day or repository, tool, timezone (days of group_by day; default: the server\'s time zone)'] },
            { cells: ['search_logs', 'Log records, newest first', 'query, tool, severity, trace_id, from (24 hours), to, limit (20, max 100), cursor'] },
            { cells: ['list_traces', 'Traces, newest first: root span, duration, span count, status. Leaves out Codex\'s background traces (internal requests outside a conversation, automatic approval reviews)', 'query, tool, from (24 hours), to, limit, cursor, include_background (false)'] },
            { cells: ['get_trace', 'A trace\'s spans as a tree (depth first): name, start, duration, status, attributes. Of traces with more than 1,000 spans, the tree of the first 1,000 by start time', 'id, kind (otel_trace or codex_operation), max_spans (200), max_chars per attribute (300)'] },
            { cells: ['list_metric_names', 'Stored metric names', 'tool'] },
            { cells: ['query_metrics', 'A metric as time buckets or totals per series (service and token type, and model for totals; or the group_by attributes); cumulative counters become increases; histograms (durations, sizes) show their average or the stat asked for, percentiles estimated from their buckets', 'name, tool, from (24 hours), to, aggregate, interval (5m, 1h, 1d, 1w; about 50 buckets), group_by (up to 2 attributes), filters (exact attribute values), stat (avg, sum, count, min, max, p50, p90, p95, p99), timezone (default: the server\'s)'] },
          ],
        },
      },
      {
        type: 'paragraph',
        text: 'Results are JSON (also as structured content with an output schema). Long text is cut and marked ("… [1234 more characters]", "truncated": true), so a single result does not fill the agent\'s context. Invalid arguments return a tool error that explains what to pass instead.',
      },
      {
        type: 'paragraph',
        text: 'usage_summary reports what AI Observer recorded: a Claude Code, Codex or Gemini CLI request recorded both over OTLP and in its session file counts once, and repository is known for imported and watched Claude Code and Codex sessions only (others show unknown). tokens lists every token type; total_tokens counts each token once, so it leaves out the types that another one includes: cached input of Codex, Gemini and GitHub Copilot and Codex\'s cache writes (part of their input), and Codex reasoning tokens (part of its output).',
      },
      {
        type: 'heading',
        level: 3,
        text: 'Searching Conversations',
      },
      {
        type: 'paragraph',
        text: 'search_conversations splits query into words at spaces and finds the prompts, replies and tool calls that contain all of them, in any order:',
      },
      {
        type: 'table',
        table: {
          headers: ['Query', 'Finds records that contain'],
          rows: [
            { cells: ['flaky parser test', 'flaky, parser and test, e.g. "the parser test is flaky on CI"'] },
            { cells: ['retr backoff', 'retry, retries or retrying, and backoff'] },
            { cells: ['"not found" timeout', 'the phrase not found exactly as written, and timeout'] },
            { cells: ['ErrOffsetTooLarge', 'the identifier, also inside longer text such as storage.ErrOffsetTooLarge'] },
          ],
        },
      },
      {
        type: 'list',
        items: [
          'Case and word parts: case is ignored, and a word also matches inside longer words: retr finds retry, test finds parser_test.go.',
          'Same record: all words must appear in the same prompt, reply or tool call: in its text or the values of its attributes, such as the command of a tool call, but not in attribute names (id does not match every record\'s session.id). A session whose words are spread over different messages does not match; search for fewer words, or for each one separately.',
          'Phrases: text in double quotes is one term and matches only as written, spaces included.',
          'Limits: at most 10 terms; repeated words count once. The result lists the terms the query was split into.',
          'Text, not meaning: flaky does not find intermittent, and retries does not find retry (the shorter retr finds both).',
          'Order of results: sessions with the newest matching record come first. matches counts a session\'s matching records in the range, and snippet shows the text around the words in the newest one.',
        ],
      },
      {
        type: 'heading',
        level: 3,
        text: 'Transcripts as Resources',
      },
      {
        type: 'paragraph',
        text: 'The transcript of a session is also a resource, ai-observer://sessions/{session_id}/transcript (Markdown), for clients that attach resources to a prompt.',
      },
      {
        type: 'heading',
        level: 3,
        text: 'Charts in MCP Apps Hosts',
      },
      {
        type: 'paragraph',
        text: 'Hosts that render MCP Apps show the results of four tools as charts and tables in the conversation, besides the text the agent reads:',
      },
      {
        type: 'table',
        table: {
          headers: ['Tool', 'View'],
          rows: [
            { cells: ['usage_summary', 'Total cost and tokens; bars per tool, model or repository, or daily bars stacked by tool. Switch between cost and tokens. Hosts that support tool calls can change the grouping for the same range'] },
            { cells: ['query_metrics', 'The metric over time (bars for counters, lines for other metrics), or bars of the totals with aggregate'] },
            { cells: ['get_trace', 'The trace page\'s waterfall; click a span for its attributes and error'] },
            { cells: ['list_sessions', 'Counts per tool and recorded cost for the loaded sessions, with title, repository, branch and cost for each row; Load more reads the next page'] },
          ],
        },
      },
      {
        type: 'list',
        items: [
          'Which agents show it: hosts that support MCP Apps and run on your machine, such as the Codex app (with apps = true and enable_mcp_apps = true under [features] in ~/.codex/config.toml), VS Code with GitHub Copilot (turn on the experimental setting chat.mcp.apps.enabled), Cursor and Goose. The host must reach the endpoint: one that connects from the cloud cannot reach 127.0.0.1. Claude Code, Codex CLI, Gemini CLI and OpenCode don\'t render MCP Apps; they get the same results as before, and nothing of the view enters their context.',
          'Claude Desktop doesn\'t show the view of a local AI Observer. It connects to servers added by URL (Connectors) from Anthropic\'s cloud, which can\'t reach 127.0.0.1. Servers from its configuration file (claude_desktop_config.json, for AI Observer through mcp-remote) work, but their tools answer as text only: in a test with app version 4.23118.0 (November 2026) Claude never loaded the view, and Anthropic\'s own MCP Apps example (@modelcontextprotocol/server-map) showed no view either.',
          'How it works: clients advertising the MCP Apps HTML profile receive the view ui://ai-observer/view.html in their _meta.ui.resourceUri. The host reads it, a single page with its scripts and styles inlined, built into the binary, and shows it in a sandboxed frame with the tool\'s arguments and result.',
          'The view has no network access. It calls tools through the host (another grouping, the next page), so the endpoint\'s Host, Origin and token checks apply as to any call.',
          'The agent still gets the whole result as text and may repeat it below the view. The MCP Apps specification has no setting against that; the page carries OpenAI\'s openai/widgetDescription, which describes the visible summary and the initial 20-row limit, and other hosts ignore it. To be sure, tell the agent in its global instructions (~/.codex/AGENTS.md, ~/.claude/CLAUDE.md): "When an MCP tool result is shown as an app view, don\'t restate the data it shows."',
          'resources/list leaves out ui:// resources, as the MCP Apps specification allows: hosts find the view through the tools, while agents that browse resources (Claude Code and Codex have tools for that) would otherwise read the page, about 1 MB, into their context.',
        ],
      },
      {
        type: 'heading',
        level: 3,
        text: 'Security',
      },
      {
        type: 'list',
        items: [
          'What agents see: the stored prompts, code, tool output and file paths. Whatever a tool returns is sent to the agent\'s model provider. Enable MCP only when you are comfortable with that, and keep secret redaction on (AI_OBSERVER_REDACT, default secrets; see Redaction below). Results are redacted again when they are returned, so telemetry stored before redaction was enabled is covered too.',
          'Who can connect: the endpoint is behind the same checks as the API: the Host check (DNS rebinding protection), the Origin check, and with AI_OBSERVER_AUTH_TOKEN a bearer token. Like the dashboard, it listens on 127.0.0.1 unless AI_OBSERVER_HOST says otherwise.',
          'No writes, no SQL: the tools only read, and there is no tool that runs SQL.',
        ],
      },
      {
        type: 'heading',
        level: 3,
        text: 'Behavior',
      },
      {
        type: 'list',
        items: [
          'Transport: Streamable HTTP with JSON responses. Legacy MCP Apps clients receive an Mcp-Session-Id during initialization and can close it with DELETE /mcp. Idle sessions expire after 30 minutes. Other requests are stateless. GET /mcp returns 405.',
          'No request timeout: MCP requests are exempt from the API\'s 5-second timeout, so a search over a large database can finish. A request ends when the client cancels it or AI Observer shuts down.',
          'Search speed: search_conversations scans the text of every session log and span in the range. In a benchmark with 2 million log records over 30 days (on a laptop) it took about 0.2 s for 7 days and 0.5–0.7 s for the default 30 days. Additional words cost little when one of them is rare: longer words are checked first, since they usually are rarer, and the others only in the records that contain it. Three words that each appear in many records took about twice as long as one. Narrow from/to or pass tool for faster searches.',
        ],
      },
    ],
  },
  {
    id: 'api',
    title: 'REST API',
    subsections: [
      { id: 'api-endpoints', title: 'Endpoints' },
      { id: 'api-metrics', title: 'Metric Series' },
      { id: 'api-sessions', title: 'Sessions and Transcripts' },
      { id: 'api-dashboards', title: 'Dashboards and Widgets' },
      { id: 'api-operations', title: 'Health, Monitoring and Maintenance' },
    ],
    content: [
      {
        type: 'paragraph',
        text: 'The dashboard reads everything through the REST API on the API port (http://localhost:8080/api); scripts can use it too. With AI_OBSERVER_AUTH_TOKEN set, send Authorization: Bearer <token>. Times are RFC 3339; to defaults to now and from to 24 hours before to. Lists return total, hasMore and, when there are more, nextCursor: pass it as cursor for the next page; empty lists are []. Invalid parameters answer 400 with a message naming them; limit must be a positive number, and values above an endpoint\'s maximum are reduced to it. docs/openapi.json in the repository describes every endpoint, parameter and response type (OpenAPI 3.1, generated from the server\'s types).',
      },
      {
        type: 'heading',
        level: 3,
        text: 'Endpoints',
      },
      {
        type: 'table',
        table: {
          headers: ['Endpoint', 'Returns', 'Parameters'],
          rows: [
            { cells: ['GET /api/status', 'Version, commit, build date, mode (serve, watch or serve+watch, with watchedTools), start time; listen address and ports (otlpEnabled is false in watch mode; otlpGrpcPort, otlpGrpcEnabled); database path, size, WAL size, usedBytes, freeBytes, compactRecommended, growthBytesPerDay, memory limit, threads; stored telemetry counts with oldest and newest record; settings (retentionDays, maxDbSizeBytes, redaction, authEnabled, tlsEnabled, mcpEnabled, adminApiEnabled, backupDir, storeAllSpans, logLevel, logFormat, corsOrigin, allowedHosts, skipMigrationBackup); pricing files with lastUpdated and overrides; imported files per tool', '—'] },
            { cells: ['GET /api/sessions', 'Sessions with logs in the range, most recently active first, each with its whole extent: start, last activity, message count, model, title (the first prompt), repository and branch (imported Claude Code and Codex sessions), cost and tokens when recorded', 'service, from, to, search, limit (50, max 1,000), cursor'] },
            { cells: ['GET /api/sessions/{id}/transcript', 'A page of the session\'s messages, oldest first, with title and totals over all messages', 'limit (500, max 1,000), cursor, search'] },
            { cells: ['GET /api/logs', 'Log records, newest first; sessionId names the session of a record', 'service, severity, traceId, search, from, to, limit, cursor'] },
            { cells: ['GET /api/traces, /api/traces/recent', 'Trace rows, each describing the whole trace (all of its spans), ordered by the trace\'s first span in the range; sessionId is set when all spans with a session share one', 'service, search, background, from, to, limit, cursor'] },
            { cells: ['GET /api/traces/{id}/spans', 'A page of a trace\'s spans (1,000 at most), each with its sessionId', 'kind, limit, cursor'] },
            { cells: ['GET /api/metrics', 'Stored metric data points, newest first (what the Data points tab shows)', 'service, name, type, from, to, limit, cursor'] },
            { cells: ['GET /api/metrics/series', 'A metric as time buckets per series; cumulative counters become increases', 'name, service, from, to, interval (seconds), aggregate, groupBy, attr.<key>, stat, merge, tz'] },
            { cells: ['POST /api/metrics/batch-series', 'Several series over one range; each query succeeds or fails on its own (error and code)', 'body: from, to, interval, timeZone, queries [{id, name, service, aggregate, groupBy, filters, stat, merge}] (at most 50)'] },
            { cells: ['GET /api/metrics/attribute-keys', 'The attribute keys of a metric\'s points, which series can be grouped and filtered by', 'name, service'] },
          ],
        },
      },
      {
        type: 'paragraph',
        text: 'A sessionId from logs, spans or traces can be opened as /sessions/{sessionId} in the dashboard and read with the transcript endpoint, which also accepts the other IDs of a session.',
      },
      {
        type: 'heading',
        level: 3,
        text: 'Metric Series',
      },
      {
        type: 'list',
        items: [
          'interval: the bucket size in whole seconds, 1 to 2,678,400 (31 days), default 60; other values answer 400. Ranges that would need more than about 2,000 buckets get a coarser interval, which the response names.',
          'groupBy: up to two attribute keys, comma-separated (model, or model,repository): one series per service and value instead of per token type. A bucketed query returns at most 100 series.',
          'attr.<key>=<value>: keep only points whose attribute has the value, such as attr.model=claude-sonnet-5-5 (at most 10; filters in batch queries). merge=true: one series of all matching points.',
          'stat: the statistic of a histogram metric, avg (default), sum, count, min, max, p50, p90, p95 or p99 (percentiles estimated from the buckets). Buckets without points are null for gauges and for a histogram\'s average, extremes and percentiles.',
          'tz: the IANA time zone buckets follow, such as Europe/Berlin (default UTC); the dashboard sends the browser\'s, so a daily bucket is a local day.',
        ],
      },
      {
        type: 'heading',
        level: 3,
        text: 'Sessions and Transcripts',
      },
      {
        type: 'list',
        items: [
          'Session search: search takes words and "quoted phrases", up to 10 terms. A session matches when one of its log or span records in the range contains all terms (in its text or attribute values, case ignored, also inside longer words). Results come newest match first, with snippet (the text around the terms in the newest matching record), matches (matching records in the range) and matchedAt; total counts the matching sessions. A query of only quotes or with more than 10 terms answers 400 with an explanation.',
          'Title: the first prompt of the session, one line of at most 200 characters, redacted. Sessions known from spans only (GitHub Copilot) have none.',
          'Totals: every transcript page carries totals over all messages of the session, not only the page: costUsd, inputTokens, outputTokens, cacheRead, cacheWrite, userMessages, assistantMessages, toolCalls and toolErrors (tool results that failed).',
          'Transcript search: with search, a message matches when every term is in its content, tool name, tool input or tool output. search.matches lists the positions of all matching messages in the whole transcript (0-based); with page size L, position p is on page p / L (rounded down). Load pages in order until that page has loaded.',
          'Unknown sessions answer 404; known sessions without content return an empty transcript.',
        ],
      },
      {
        type: 'heading',
        level: 3,
        text: 'Dashboards and Widgets',
      },
      {
        type: 'list',
        items: [
          'GET /api/dashboards/default always returns a dashboard: when there is none, the server creates the Default Dashboard with its six widgets, once, also when several browser tabs ask at the same time.',
          'PUT /api/dashboards/{id}/widgets/{widgetId} changes a widget and returns it whole. The body may hold title, gridColumn, gridRow, colSpan, rowSpan and config. A config replaces the stored configuration whole (send all of it; {} clears it), an omitted config keeps it. Omitted or 0 numbers and an empty title keep the stored values.',
          'The server refuses colSpan above 4, negative sizes or positions, and titles over 255 characters (400), and a widget that is not on the dashboard (404). It does not check for overlapping widgets: the dashboard does before it saves.',
          'PUT /api/dashboards/{id}/widgets/positions moves several widgets at once ({"positions": [{"id", "gridColumn", "gridRow"}]}).',
        ],
      },
      {
        type: 'heading',
        level: 3,
        text: 'Health, Monitoring and Maintenance',
      },
      {
        type: 'table',
        table: {
          headers: ['Endpoint', 'Returns', 'Notes'],
          rows: [
            { cells: ['GET /health', 'Liveness: 200 while the server runs', 'API and OTLP ports, no token'] },
            { cells: ['GET /ready', 'Readiness: 200, or 503 when the database does not answer within 2 seconds', 'API and OTLP ports, no token; the Docker health check uses it'] },
            { cells: ['GET /metrics, GET /api/internal/metrics', 'AI Observer\'s own metrics (Prometheus text format, or JSON): OTLP requests, records stored and rejected, write times, API durations and timeouts, WebSocket clients, watcher files, retention, failed authentication', 'API port, behind the token'] },
            { cells: ['POST /api/admin/delete, /export, /backup, /checkpoint', 'Maintenance while the server runs (see Maintenance)', 'Only with AI_OBSERVER_ADMIN_API=true'] },
          ],
        },
      },
    ],
  },
  {
    id: 'redaction',
    title: 'Redaction',
    content: [
      {
        type: 'paragraph',
        text: 'Before telemetry is stored, secrets in it are replaced with markers such as [REDACTED:github_token]: API keys and tokens of common services, JWTs, private keys, Authorization header values, passwords in URLs, and credential values of assignments such as DB_PASSWORD=. AI_OBSERVER_REDACT selects what is redacted:',
      },
      {
        type: 'list',
        items: [
          'secrets (default): credentials only.',
          'all: credentials and personal data (emails as stable pseudonyms, payment card numbers, IBANs, phone numbers in international format, US social security numbers, public IP addresses).',
          'off: telemetry is stored as received.',
        ],
      },
      {
        type: 'note',
        text: 'Redaction applies to telemetry stored from then on, from OTLP, import, watch, export --from-files and restore (which redacts restored text again). The MCP tools also redact what they return, which covers telemetry stored earlier.',
      },
    ],
  },
  {
    id: 'telemetry',
    title: 'Telemetry Reference',
    subsections: [
      { id: 'telemetry-claude', title: 'Claude Code' },
      { id: 'telemetry-gemini', title: 'Gemini CLI' },
      { id: 'telemetry-copilot', title: 'GitHub Copilot' },
      { id: 'telemetry-opencode', title: 'OpenCode' },
      { id: 'telemetry-codex', title: 'Codex' },
    ],
    content: [
      {
        type: 'paragraph',
        text: 'AI Observer collects OpenTelemetry metrics and events and imports data from supported local session files. Metric availability and meaning depend on the ingestion mode. Each metric includes metadata for display names, descriptions, units, and breakdown attributes for multi-series visualization.',
      },
      {
        type: 'heading',
        level: 3,
        text: 'Claude Code Metrics',
      },
      {
        type: 'table',
        table: {
          headers: ['Metric Name', 'Display Name', 'Unit', 'Description'],
          rows: [
            { cells: ['claude_code.session.count', 'Sessions', 'count', 'OTLP: CLI sessions started. Full import: non-empty session files, including separate agent files'] },
            { cells: ['claude_code.lines_of_code.count', 'Lines of Code', 'count', 'Lines of code added or removed'] },
            { cells: ['claude_code.pull_request.count', 'Pull Requests', 'count', 'OTLP: pull requests created. Full import: distinct repository/PR links per file, not confirmed creations'] },
            { cells: ['claude_code.commit.count', 'Commits', 'count', 'Number of commits made'] },
            { cells: ['claude_code.cost.usage', 'Cost', 'USD', 'Total cost incurred in USD'] },
            { cells: ['claude_code.cost.usage_user_facing', 'Cost (User-Facing)', 'USD', 'Cost for user-facing API calls only (excludes tool-routing)'] },
            { cells: ['claude_code.token.usage', 'Token Usage', 'tokens', 'Token consumption by type'] },
            { cells: ['claude_code.token.usage_user_facing', 'Token Usage (User-Facing)', 'tokens', 'Tokens for user-facing API calls only (excludes tool-routing)'] },
            { cells: ['claude_code.code_edit_tool.decision', 'Edit Decisions', 'count', 'Code edit tool usage decisions'] },
            { cells: ['claude_code.active_time.total', 'Active Time', 'seconds', 'OTLP: active time. Full import: recorded turn durations converted to seconds, excluding user typing/reading time'] },
          ],
        },
      },
      {
        type: 'paragraph',
        text: 'Full Claude Code imports reconstruct lines of code and commits from supported successful tool results. Failed or incomplete tool activity is omitted. File overwrites need original contents or a recorded patch to calculate line changes; commits need a parsed git commit command and a matching completion result. These are historical reconstructions of the evidence in the file.',
      },
      {
        type: 'note',
        text: 'Activity metrics are available from full imports, not incremental watch mode. The watcher imports transcripts and token/cost usage only. Imported metrics carry import_source=local_jsonl; PR links and turn durations also identify their approximation with reconstruction=pr_link and reconstruction=turn_duration. Avoid adding overlapping imports and live telemetry as independent activity.',
      },
      {
        type: 'heading',
        level: 3,
        text: 'Gemini CLI Metrics',
      },
      {
        type: 'table',
        table: {
          headers: ['Metric Name', 'Display Name', 'Unit', 'Description'],
          rows: [
            { cells: ['gemini_cli.session.count', 'Sessions', 'count', 'Number of CLI sessions'] },
            { cells: ['gemini_cli.cost.usage', 'Cost', 'USD', 'Total cost in USD for Gemini CLI usage'] },
            { cells: ['gemini_cli.tool.call.count', 'Tool Calls', 'count', 'Number of tool invocations'] },
            { cells: ['gemini_cli.tool.call.latency', 'Tool Latency', 'ms', 'Tool call execution time'] },
            { cells: ['gemini_cli.tool.queue.depth', 'Tool Queue Depth', 'count', 'Number of pending tools in queue'] },
            { cells: ['gemini_cli.tool.execution.breakdown', 'Tool Execution Breakdown', 'ms', 'Phase-level tool execution durations'] },
            { cells: ['gemini_cli.api.request.count', 'API Requests', 'count', 'Number of API requests made'] },
            { cells: ['gemini_cli.api.request.latency', 'API Latency', 'ms', 'API request latency'] },
            { cells: ['gemini_cli.api.request.breakdown', 'API Request Breakdown', 'ms', 'Request phase timing analysis'] },
            { cells: ['gemini_cli.token.usage', 'Token Usage', 'tokens', 'Token consumption by type (input includes cache)'] },
            { cells: ['gemini_cli.token.efficiency', 'Token Efficiency', 'ratio', 'Output quality metrics ratio'] },
            { cells: ['gemini_cli.file.operation.count', 'File Operations', 'count', 'File read/write operations'] },
            { cells: ['gemini_cli.lines.changed', 'Lines Changed', 'count', 'Code lines modified'] },
            { cells: ['gemini_cli.agent.run.count', 'Agent Runs', 'count', 'Number of agent executions'] },
            { cells: ['gemini_cli.agent.duration', 'Agent Duration', 'ms', 'Agent execution time'] },
            { cells: ['gemini_cli.agent.turns', 'Agent Turns', 'count', 'Conversation turns per agent'] },
            { cells: ['gemini_cli.chat_compression', 'Chat Compression', 'count', 'Chat message compression events'] },
            { cells: ['gemini_cli.chat.invalid_chunk.count', 'Invalid Chunks', 'count', 'Malformed stream data count'] },
            { cells: ['gemini_cli.chat.content_retry.count', 'Content Retries', 'count', 'Recovery attempt count'] },
            { cells: ['gemini_cli.chat.content_retry_failure.count', 'Retry Failures', 'count', 'Exhausted retry attempts count'] },
            { cells: ['gemini_cli.slash_command.model.call_count', 'Model Commands', 'count', 'Model selections via slash commands'] },
            { cells: ['gemini_cli.model_routing.latency', 'Routing Latency', 'ms', 'Router decision timing'] },
            { cells: ['gemini_cli.model_routing.failure.count', 'Routing Failures', 'count', 'Model routing failure count'] },
            { cells: ['gemini_cli.startup.duration', 'Startup Duration', 'ms', 'CLI startup time'] },
            { cells: ['gemini_cli.memory.usage', 'Memory Usage', 'bytes', 'Memory consumption'] },
            { cells: ['gemini_cli.cpu.usage', 'CPU Usage', '%', 'CPU utilization'] },
            { cells: ['gemini_cli.ui.flicker.count', 'UI Flicker', 'count', 'Rendering instability events'] },
            { cells: ['gemini_cli.performance.score', 'Performance Score', 'score', 'Composite performance benchmark'] },
            { cells: ['gemini_cli.performance.regression', 'Performance Regressions', 'count', 'Performance degradation events'] },
            { cells: ['gemini_cli.performance.baseline.comparison', 'Baseline Comparison', '%', 'Performance baseline drift'] },
            { cells: ['gen_ai.client.token.usage', 'GenAI Token Usage', 'tokens', 'Generic AI token usage (OTel convention)'] },
            { cells: ['gen_ai.client.operation.duration', 'GenAI Op Duration', 'seconds', 'Generic AI operation duration (OTel convention)'] },
          ],
        },
      },
      {
        type: 'note',
        text: 'Gemini CLI Derived Metrics',
      },
      {
        type: 'paragraph',
        text: 'Gemini CLI sends cumulative counters; their charts show the increase per interval, computed per series. Cost charges cached tokens at the cache rate only. The .delta names of earlier versions are no longer listed, but widgets that use them keep working and return the same per-interval series:',
      },
      {
        type: 'table',
        table: {
          headers: ['Metric Name', 'Display Name', 'Description'],
          rows: [
            { cells: ['gemini_cli.session.count.delta', 'Sessions', 'Sessions per interval'] },
            { cells: ['gemini_cli.token.usage.delta', 'Token Usage', 'Tokens consumed per interval'] },
            { cells: ['gemini_cli.api.request.count.delta', 'API Requests', 'API requests per interval'] },
            { cells: ['gemini_cli.file.operation.count.delta', 'File Operations', 'File operations per interval'] },
          ],
        },
      },
      {
        type: 'heading',
        level: 3,
        text: 'GitHub Copilot Metrics',
      },
      {
        type: 'paragraph',
        text: 'GitHub Copilot exports OTLP telemetry from the VS Code extension and CLI. AI Observer stores the raw OTLP data and derives token and cost metrics from chat spans.',
      },
      {
        type: 'note',
        text: 'Service names: copilot-chat identifies the GitHub Copilot VS Code Extension, and github-copilot identifies GitHub Copilot CLI. They remain separate services for filtering and stored telemetry, but the frontend groups their metrics under one GitHub Copilot provider family because they share the same metric catalog and pricing model.',
      },
      {
        type: 'note',
        text: 'Model aliases: Copilot cost derivation uses a snapshot of the GitHub Models catalog API to match emitted model IDs, display names, and dated versions such as gpt-4o-mini-2024-07-18. The catalog does not include prices, so AI Observer emits cost rows only when a catalog alias can be matched to known Copilot or source-provider pricing.',
      },
      {
        type: 'table',
        table: {
          headers: ['Metric Name', 'Display Name', 'Unit', 'Description'],
          rows: [
            { cells: ['github_copilot.token.usage', 'Token Usage', 'tokens', 'Derived token usage by type and model'] },
            { cells: ['github_copilot.cost.usage', 'Cost', 'USD', 'Estimated cost derived from token usage and model pricing'] },
            { cells: ['gen_ai.client.operation.duration', 'GenAI Op Duration', 'seconds', 'Generic AI operation duration (OTel convention)'] },
            { cells: ['gen_ai.client.token.usage', 'GenAI Token Usage', 'tokens', 'Generic AI token usage (OTel convention)'] },
            { cells: ['copilot_chat.tool.call.count', 'Tool Calls', 'count', 'Number of Copilot tool calls'] },
            { cells: ['copilot_chat.tool.call.duration', 'Tool Duration', 'seconds', 'Tool call execution time'] },
            { cells: ['copilot_chat.agent.invocation.duration', 'Agent Invocation Duration', 'seconds', 'Agent invocation execution time'] },
            { cells: ['copilot_chat.agent.turn.count', 'Agent Turns', 'count', 'Number of agent turns'] },
            { cells: ['copilot_chat.session.count', 'Sessions', 'count', 'Number of Copilot chat sessions'] },
            { cells: ['copilot_chat.time_to_first_token', 'Time to First Token', 'seconds', 'Time until the first response token'] },
            { cells: ['copilot_chat.edit.acceptance.count', 'Edit Acceptance', 'count', 'Accepted or rejected edit count'] },
            { cells: ['copilot_chat.chat_edit.outcome.count', 'Chat Edit Outcomes', 'count', 'Chat edit outcome count'] },
            { cells: ['copilot_chat.lines_of_code.count', 'Lines of Code', 'count', 'Lines of code changed'] },
            { cells: ['copilot_chat.edit.survival.four_gram', 'Edit Survival (Four-Gram)', 'ratio', 'Edit survival ratio using four-gram matching'] },
            { cells: ['copilot_chat.edit.survival.no_revert', 'Edit Survival (No Revert)', 'ratio', 'Edit survival ratio based on non-reverted edits'] },
            { cells: ['copilot_chat.user.action.count', 'User Actions', 'count', 'User action count'] },
            { cells: ['copilot_chat.user.feedback.count', 'User Feedback', 'count', 'User feedback count'] },
            { cells: ['copilot_chat.agent.edit_response.count', 'Agent Edit Responses', 'count', 'Agent edit response count'] },
            { cells: ['copilot_chat.agent.summarization.count', 'Agent Summarizations', 'count', 'Agent summarization count'] },
            { cells: ['copilot_chat.pull_request.count', 'Pull Requests', 'count', 'Pull request event count'] },
            { cells: ['copilot_chat.cloud.session.count', 'Cloud Sessions', 'count', 'Cloud session count'] },
            { cells: ['copilot_chat.cloud.pr_ready.count', 'Cloud PR Ready', 'count', 'Cloud pull requests marked ready'] },
          ],
        },
      },
      {
        type: 'paragraph',
        text: 'Copilot session transcripts combine supported log events with conversation content captured in GenAI spans. They include user messages, assistant replies, tool calls, and tool results. Related child spans can be included even when their session ID is missing or uses a supported alternate ID, while overlapping telemetry is combined without duplicating messages or token usage.',
      },
      {
        type: 'note',
        text: 'To capture conversation content, enable github.copilot.chat.otel.captureContent in VS Code or COPILOT_OTEL_CAPTURE_CONTENT=true for the environment-based setup, and send traces to AI Observer. A session may be listed without any captured conversation; its transcript then shows "No messages in this session". Upgrading can display content already stored in spans, but enabling capture later cannot recover content that was never recorded.',
      },
      {
        type: 'heading',
        level: 3,
        text: 'OpenCode Metrics',
      },
      {
        type: 'paragraph',
        text: 'OpenCode exports OTLP telemetry through the @devtheops/opencode-plugin-otel plugin. AI Observer stores the raw OTLP data and surfaces the plugin-native token and cost metrics without deriving additional pricing rows.',
      },
      {
        type: 'note',
        text: 'Service name: opencode identifies OpenCode telemetry from the OTEL plugin. OpenCode is OTLP-only in AI Observer; there is no file watcher or historical file import parser.',
      },
      {
        type: 'table',
        table: {
          headers: ['Metric Name', 'Display Name', 'Unit', 'Description'],
          rows: [
            { cells: ['opencode.session.count', 'Sessions', 'count', 'Number of OpenCode sessions started'] },
            { cells: ['opencode.token.usage', 'Token Usage', 'tokens', 'Plugin-native token usage by type and model'] },
            { cells: ['opencode.cost.usage', 'Cost', 'USD', 'Plugin-native cost for completed assistant messages'] },
            { cells: ['opencode.lines_of_code.count', 'Lines of Code', 'count', 'Gross positive line churn by additions and deletions'] },
            { cells: ['opencode.lines_of_code.total', 'Lines of Code Total', 'count', 'Current cumulative line changes for the session'] },
            { cells: ['opencode.commit.count', 'Commits', 'count', 'Git commits detected by OpenCode'] },
            { cells: ['opencode.tool.duration', 'Tool Duration', 'ms', 'Tool execution time'] },
            { cells: ['opencode.cache.count', 'Cache Activity', 'count', 'Cache read and cache creation activity'] },
            { cells: ['opencode.session.duration', 'Session Duration', 'ms', 'Session duration from created to idle'] },
            { cells: ['opencode.message.count', 'Messages', 'count', 'Completed assistant messages'] },
            { cells: ['opencode.session.token.total', 'Session Token Total', 'tokens', 'Total tokens consumed per session'] },
            { cells: ['opencode.session.cost.total', 'Session Cost Total', 'USD', 'Total cost per session'] },
            { cells: ['opencode.model.usage', 'Model Usage', 'count', 'Messages by model and provider'] },
            { cells: ['opencode.retry.count', 'Retries', 'count', 'API retries observed from session status events'] },
          ],
        },
      },
      {
        type: 'note',
        text: 'OpenCode events: AI Observer recognizes session.created, session.idle, session.error, user_prompt, api_request, api_error, tool_result, tool_decision, and commit for session and transcript views.',
      },
      {
        type: 'heading',
        level: 3,
        text: 'Codex Metrics & Events',
      },
      {
        type: 'paragraph',
        text: 'Codex exports logs, traces and (since 0.167) metrics. Codex 0.167 and later send three service names, which AI Observer counts as Codex: codex_cli_rs (the CLI\'s startup telemetry), codex-app-server (conversations of the Codex app and the CLI; originator is Codex_Desktop or codex-tui) and codex-exec-server (the app\'s command runner). AI Observer derives the following metrics from their response.completed events:',
      },
      {
        type: 'note',
        text: 'Codex Derived Metrics',
      },
      {
        type: 'table',
        table: {
          headers: ['Metric Name', 'Display Name', 'Unit', 'Description'],
          rows: [
            { cells: ['codex_cli_rs.token.usage', 'Token Usage', 'tokens', 'Tokens by type: input (includes cacheRead and cacheWrite), output (includes reasoning), cacheRead, cacheWrite, reasoning; by model and originator'] },
            { cells: ['codex_cli_rs.cost.usage', 'Cost', 'USD', 'Cost in USD by model and originator; models without a price (such as codex-auto-review, the automatic approval reviewer) have none'] },
          ],
        },
      },
      {
        type: 'note',
        text: 'Codex Events',
      },
      {
        type: 'table',
        table: {
          headers: ['Event Name', 'Display Name', 'Description'],
          rows: [
            { cells: ['codex.conversation_starts', 'Sessions', 'Conversation session started'] },
            { cells: ['codex.api_request', 'API Requests', 'API request made'] },
            { cells: ['codex.user_prompt', 'User Prompts', 'User prompt submitted'] },
            { cells: ['codex.tool_decision', 'Tool Decisions', 'Tool execution decisions'] },
            { cells: ['codex.tool_result', 'Tool Results', 'Tool execution outcomes'] },
            { cells: ['codex.sse_event', 'SSE Events', 'Streaming response lifecycle events'] },
          ],
        },
      },
      {
        type: 'heading',
        level: 3,
        text: 'Breakdown Attributes',
      },
      {
        type: 'paragraph',
        text: 'Many metrics support breakdown attributes for multi-series visualization. When viewing a chart, you can split the data by various dimensions:',
      },
      {
        type: 'list',
        items: [
          'Token Usage metrics - breakdown by type (input, output, cache) or by model',
          'Lines Changed metrics - breakdown by type (added vs removed)',
          'Tool metrics - breakdown by function_name, decision, or success status',
          'API metrics - breakdown by model, status_code, or error_type',
        ],
      },
      {
        type: 'note',
        text: 'The breakdown attribute can be configured per-widget in the dashboard. If not specified, the default breakdown (usually type) is used.',
      },
    ],
  },
]

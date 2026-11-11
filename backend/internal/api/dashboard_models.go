package api

import "time"

// Dashboard represents a user-defined dashboard
type Dashboard struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	IsDefault   bool      `json:"isDefault"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// DashboardWidget represents a widget placed on a dashboard
type DashboardWidget struct {
	ID          string       `json:"id"`
	DashboardID string       `json:"dashboardId"`
	WidgetType  string       `json:"widgetType"`
	Title       string       `json:"title"`
	GridColumn  int          `json:"gridColumn"`
	GridRow     int          `json:"gridRow"`
	ColSpan     int          `json:"colSpan"`
	RowSpan     int          `json:"rowSpan"`
	Config      WidgetConfig `json:"config"`
	CreatedAt   time.Time    `json:"createdAt"`
	UpdatedAt   time.Time    `json:"updatedAt"`
}

// WidgetConfig holds optional configuration for configurable widgets
type WidgetConfig struct {
	Service    string `json:"service,omitempty"`
	MetricName string `json:"metricName,omitempty"`
	// BreakdownAttribute is the attribute key series are broken down by
	// (such as type or model)
	BreakdownAttribute string `json:"breakdownAttribute,omitempty"`
	// BreakdownValue is the value of BreakdownAttribute a metric value
	// widget shows
	BreakdownValue string `json:"breakdownValue,omitempty"`
	// ChartStacked stacks a chart's bars (default true)
	ChartStacked *bool `json:"chartStacked,omitempty"`
	// Stat is the statistic of a histogram metric (MetricQuery.Stat)
	Stat string `json:"stat,omitempty"`
}

// DashboardWithWidgets represents a full dashboard with its widgets
type DashboardWithWidgets struct {
	Dashboard
	Widgets []DashboardWidget `json:"widgets"`
}

// CreateDashboardRequest is the body of POST /api/dashboards
type CreateDashboardRequest struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	IsDefault   bool   `json:"isDefault,omitempty"`
}

// UpdateDashboardRequest changes the name and description of a dashboard
// (those it sets)
type UpdateDashboardRequest struct {
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
}

// CreateWidgetRequest is the body of POST /api/dashboards/{id}/widgets
type CreateWidgetRequest struct {
	WidgetType string       `json:"widgetType"`
	Title      string       `json:"title"`
	GridColumn int          `json:"gridColumn"`
	GridRow    int          `json:"gridRow"`
	ColSpan    int          `json:"colSpan"`
	RowSpan    int          `json:"rowSpan"`
	Config     WidgetConfig `json:"config,omitempty"`
}

// UpdateWidgetRequest changes the fields of a widget that it sets: a
// non-empty title, positions and spans above 0, and the config when present
// (it replaces the stored config whole; {} clears it)
type UpdateWidgetRequest struct {
	Title      string        `json:"title,omitempty"`
	GridColumn int           `json:"gridColumn,omitempty"`
	GridRow    int           `json:"gridRow,omitempty"`
	ColSpan    int           `json:"colSpan,omitempty"`
	RowSpan    int           `json:"rowSpan,omitempty"`
	Config     *WidgetConfig `json:"config,omitempty"`
}

// DashboardGridColumns is the number of columns of the dashboard grid
// (GRID_COLUMNS in the frontend): widgets span at most this many
const DashboardGridColumns = 4

// WidgetPosition moves a widget to a grid cell
type WidgetPosition struct {
	ID         string `json:"id"`
	GridColumn int    `json:"gridColumn"`
	GridRow    int    `json:"gridRow"`
}

// UpdateWidgetPositionsRequest moves widgets of a dashboard
type UpdateWidgetPositionsRequest struct {
	Positions []WidgetPosition `json:"positions"`
}

// DashboardsResponse lists the dashboards (without widgets)
type DashboardsResponse struct {
	Dashboards []Dashboard `json:"dashboards"`
}

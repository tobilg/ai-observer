package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/tobilg/ai-observer/internal/api"
)

var (
	ErrDashboardNotFound = errors.New("dashboard not found")
	ErrWidgetNotFound    = errors.New("widget not found")
)

// Dashboard CRUD operations

func (s *DuckDBStore) CreateDashboard(ctx context.Context, req *api.CreateDashboardRequest) (*api.Dashboard, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	id := uuid.New().String()
	now := time.Now()

	// If setting as default, unset any existing default
	if req.IsDefault {
		if _, err := s.db.ExecContext(ctx, "UPDATE dashboards SET is_default = FALSE WHERE is_default = TRUE"); err != nil {
			return nil, fmt.Errorf("unsetting default: %w", err)
		}
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO dashboards (id, name, description, is_default, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, id, req.Name, req.Description, req.IsDefault, now, now)
	if err != nil {
		return nil, fmt.Errorf("inserting dashboard: %w", err)
	}

	return &api.Dashboard{
		ID:          id,
		Name:        req.Name,
		Description: req.Description,
		IsDefault:   req.IsDefault,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, nil
}

func (s *DuckDBStore) GetDashboards(ctx context.Context) ([]api.Dashboard, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, description, is_default, created_at, updated_at
		FROM dashboards
		ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("querying dashboards: %w", err)
	}
	defer rows.Close()

	var dashboards []api.Dashboard
	for rows.Next() {
		var d api.Dashboard
		var desc sql.NullString
		if err := rows.Scan(&d.ID, &d.Name, &desc, &d.IsDefault, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning dashboard: %w", err)
		}
		d.Description = desc.String
		dashboards = append(dashboards, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating dashboards: %w", err)
	}

	return dashboards, nil
}

func (s *DuckDBStore) GetDashboard(ctx context.Context, id string) (*api.Dashboard, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var d api.Dashboard
	var desc sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT id, name, description, is_default, created_at, updated_at
		FROM dashboards WHERE id = ?
	`, id).Scan(&d.ID, &d.Name, &desc, &d.IsDefault, &d.CreatedAt, &d.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("querying dashboard: %w", err)
	}
	d.Description = desc.String
	return &d, nil
}

func (s *DuckDBStore) GetDefaultDashboard(ctx context.Context) (*api.DashboardWithWidgets, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var d api.Dashboard
	var desc sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT id, name, description, is_default, created_at, updated_at
		FROM dashboards WHERE is_default = TRUE
	`).Scan(&d.ID, &d.Name, &desc, &d.IsDefault, &d.CreatedAt, &d.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("querying default dashboard: %w", err)
	}
	d.Description = desc.String

	widgets, err := s.getWidgetsForDashboardLocked(ctx, d.ID)
	if err != nil {
		return nil, err
	}

	return &api.DashboardWithWidgets{
		Dashboard: d,
		Widgets:   widgets,
	}, nil
}

func (s *DuckDBStore) GetDashboardWithWidgets(ctx context.Context, id string) (*api.DashboardWithWidgets, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var d api.Dashboard
	var desc sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT id, name, description, is_default, created_at, updated_at
		FROM dashboards WHERE id = ?
	`, id).Scan(&d.ID, &d.Name, &desc, &d.IsDefault, &d.CreatedAt, &d.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("querying dashboard: %w", err)
	}
	d.Description = desc.String

	widgets, err := s.getWidgetsForDashboardLocked(ctx, id)
	if err != nil {
		return nil, err
	}

	return &api.DashboardWithWidgets{
		Dashboard: d,
		Widgets:   widgets,
	}, nil
}

func (s *DuckDBStore) UpdateDashboard(ctx context.Context, id string, req *api.UpdateDashboardRequest) (*api.Dashboard, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	_, err := s.db.ExecContext(ctx, `
		UPDATE dashboards
		SET name = COALESCE(NULLIF(?, ''), name),
		    description = COALESCE(NULLIF(?, ''), description),
		    updated_at = ?
		WHERE id = ?
	`, req.Name, req.Description, now, id)
	if err != nil {
		return nil, fmt.Errorf("updating dashboard: %w", err)
	}

	var d api.Dashboard
	var desc sql.NullString
	err = s.db.QueryRowContext(ctx, `
		SELECT id, name, description, is_default, created_at, updated_at
		FROM dashboards WHERE id = ?
	`, id).Scan(&d.ID, &d.Name, &desc, &d.IsDefault, &d.CreatedAt, &d.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrDashboardNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("fetching updated dashboard: %w", err)
	}
	d.Description = desc.String
	return &d, nil
}

func (s *DuckDBStore) DeleteDashboard(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Delete widgets first
	if _, err := s.db.ExecContext(ctx, "DELETE FROM dashboard_widgets WHERE dashboard_id = ?", id); err != nil {
		return fmt.Errorf("deleting widgets: %w", err)
	}

	// Delete dashboard
	if _, err := s.db.ExecContext(ctx, "DELETE FROM dashboards WHERE id = ?", id); err != nil {
		return fmt.Errorf("deleting dashboard: %w", err)
	}

	return nil
}

func (s *DuckDBStore) SetDefaultDashboard(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	exists, err := s.dashboardExistsLocked(ctx, id)
	if err != nil {
		return err
	}
	if !exists {
		return ErrDashboardNotFound
	}

	// Unset any existing default
	if _, err := s.db.ExecContext(ctx, "UPDATE dashboards SET is_default = FALSE WHERE is_default = TRUE"); err != nil {
		return fmt.Errorf("unsetting default: %w", err)
	}

	// Set new default
	if _, err := s.db.ExecContext(ctx, "UPDATE dashboards SET is_default = TRUE, updated_at = ? WHERE id = ?", time.Now(), id); err != nil {
		return fmt.Errorf("setting default: %w", err)
	}

	return nil
}

func (s *DuckDBStore) dashboardExistsLocked(ctx context.Context, id string) (bool, error) {
	var exists bool
	if err := s.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM dashboards WHERE id = ?)", id).Scan(&exists); err != nil {
		return false, fmt.Errorf("checking dashboard exists: %w", err)
	}
	return exists, nil
}

// Widget CRUD operations

func (s *DuckDBStore) CreateWidget(ctx context.Context, dashboardID string, req *api.CreateWidgetRequest) (*api.DashboardWidget, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	exists, err := s.dashboardExistsLocked(ctx, dashboardID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrDashboardNotFound
	}

	id := uuid.New().String()
	now := time.Now()

	configJSON, err := json.Marshal(req.Config)
	if err != nil {
		return nil, fmt.Errorf("marshaling config: %w", err)
	}

	colSpan := req.ColSpan
	if colSpan == 0 {
		colSpan = 1
	}
	rowSpan := req.RowSpan
	if rowSpan == 0 {
		rowSpan = 1
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO dashboard_widgets (id, dashboard_id, widget_type, title, grid_column, grid_row, col_span, row_span, config, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id, dashboardID, req.WidgetType, req.Title, req.GridColumn, req.GridRow, colSpan, rowSpan, string(configJSON), now, now)
	if err != nil {
		return nil, fmt.Errorf("inserting widget: %w", err)
	}

	return &api.DashboardWidget{
		ID:          id,
		DashboardID: dashboardID,
		WidgetType:  req.WidgetType,
		Title:       req.Title,
		GridColumn:  req.GridColumn,
		GridRow:     req.GridRow,
		ColSpan:     colSpan,
		RowSpan:     rowSpan,
		Config:      req.Config,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, nil
}

func (s *DuckDBStore) getWidgetsForDashboardLocked(ctx context.Context, dashboardID string) ([]api.DashboardWidget, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, dashboard_id, widget_type, title, grid_column, grid_row, col_span, row_span, config, created_at, updated_at
		FROM dashboard_widgets
		WHERE dashboard_id = ?
		ORDER BY grid_row, grid_column
	`, dashboardID)
	if err != nil {
		return nil, fmt.Errorf("querying widgets: %w", err)
	}
	defer rows.Close()

	widgets := []api.DashboardWidget{}
	for rows.Next() {
		var w api.DashboardWidget
		var configJSON interface{}
		if err := rows.Scan(&w.ID, &w.DashboardID, &w.WidgetType, &w.Title, &w.GridColumn, &w.GridRow, &w.ColSpan, &w.RowSpan, &configJSON, &w.CreatedAt, &w.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning widget: %w", err)
		}
		w.Config = scanWidgetConfig(configJSON)
		widgets = append(widgets, w)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating widgets: %w", err)
	}

	return widgets, nil
}

func (s *DuckDBStore) GetWidgetsForDashboard(ctx context.Context, dashboardID string) ([]api.DashboardWidget, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.getWidgetsForDashboardLocked(ctx, dashboardID)
}

func (s *DuckDBStore) UpdateWidget(ctx context.Context, dashboardID, widgetID string, req *api.UpdateWidgetRequest) (*api.DashboardWidget, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()

	// Build update query dynamically based on provided fields
	configJSON, err := json.Marshal(req.Config)
	if err != nil {
		return nil, fmt.Errorf("marshaling config: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		UPDATE dashboard_widgets
		SET title = CASE WHEN ? != '' THEN ? ELSE title END,
		    grid_column = CASE WHEN ? > 0 THEN ? ELSE grid_column END,
		    grid_row = CASE WHEN ? > 0 THEN ? ELSE grid_row END,
		    col_span = CASE WHEN ? > 0 THEN ? ELSE col_span END,
		    row_span = CASE WHEN ? > 0 THEN ? ELSE row_span END,
		    config = ?,
		    updated_at = ?
		WHERE id = ? AND dashboard_id = ?
	`, req.Title, req.Title, req.GridColumn, req.GridColumn, req.GridRow, req.GridRow, req.ColSpan, req.ColSpan, req.RowSpan, req.RowSpan, string(configJSON), now, widgetID, dashboardID)
	if err != nil {
		return nil, fmt.Errorf("updating widget: %w", err)
	}

	// Fetch updated widget
	var w api.DashboardWidget
	var configData interface{}
	err = s.db.QueryRowContext(ctx, `
		SELECT id, dashboard_id, widget_type, title, grid_column, grid_row, col_span, row_span, config, created_at, updated_at
		FROM dashboard_widgets WHERE id = ? AND dashboard_id = ?
	`, widgetID, dashboardID).Scan(&w.ID, &w.DashboardID, &w.WidgetType, &w.Title, &w.GridColumn, &w.GridRow, &w.ColSpan, &w.RowSpan, &configData, &w.CreatedAt, &w.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrWidgetNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("fetching updated widget: %w", err)
	}
	w.Config = scanWidgetConfig(configData)
	return &w, nil
}

func (s *DuckDBStore) UpdateWidgetPositions(ctx context.Context, dashboardID string, positions []api.WidgetPosition) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	exists, err := s.dashboardExistsLocked(ctx, dashboardID)
	if err != nil {
		return err
	}
	if !exists {
		return ErrDashboardNotFound
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	now := time.Now()
	stmt, err := tx.PrepareContext(ctx, `
		UPDATE dashboard_widgets
		SET grid_column = ?, grid_row = ?, updated_at = ?
		WHERE id = ? AND dashboard_id = ?
	`)
	if err != nil {
		return fmt.Errorf("preparing statement: %w", err)
	}
	defer stmt.Close()

	for _, pos := range positions {
		result, err := stmt.ExecContext(ctx, pos.GridColumn, pos.GridRow, now, pos.ID, dashboardID)
		if err != nil {
			return fmt.Errorf("updating position for widget %s: %w", pos.ID, err)
		}
		if rows, err := result.RowsAffected(); err == nil && rows == 0 {
			return ErrWidgetNotFound
		}
	}

	return tx.Commit()
}

func (s *DuckDBStore) DeleteWidget(ctx context.Context, dashboardID, widgetID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	result, err := s.db.ExecContext(ctx, "DELETE FROM dashboard_widgets WHERE id = ? AND dashboard_id = ?", widgetID, dashboardID)
	if err != nil {
		return fmt.Errorf("deleting widget: %w", err)
	}
	if rows, err := result.RowsAffected(); err == nil && rows == 0 {
		return ErrWidgetNotFound
	}
	return nil
}

// Helper function to scan widget config from JSON.
// Errors are handled gracefully to allow partial results.
func scanWidgetConfig(v interface{}) api.WidgetConfig {
	var config api.WidgetConfig
	if v == nil {
		return config
	}

	switch val := v.(type) {
	case map[string]interface{}:
		if s, ok := val["service"].(string); ok {
			config.Service = s
		}
		if m, ok := val["metricName"].(string); ok {
			config.MetricName = m
		}
		if ba, ok := val["breakdownAttribute"].(string); ok {
			config.BreakdownAttribute = ba
		}
		if bv, ok := val["breakdownValue"].(string); ok {
			config.BreakdownValue = bv
		}
		if cs, ok := val["chartStacked"].(bool); ok {
			config.ChartStacked = &cs
		}
	case string:
		if val == "" || val == "{}" {
			return config
		}
		if err := json.Unmarshal([]byte(val), &config); err != nil {
			// Return empty config rather than failing - widget can still render
			return api.WidgetConfig{}
		}
	}
	return config
}

package storage

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/tobilg/ai-observer/internal/api"
)

func TestCreateDashboard(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	tests := []struct {
		name      string
		req       *api.CreateDashboardRequest
		wantError bool
	}{
		{
			name: "basic dashboard",
			req: &api.CreateDashboardRequest{
				Name: "Test Dashboard",
			},
		},
		{
			name: "dashboard with description",
			req: &api.CreateDashboardRequest{
				Name:        "Dashboard with Description",
				Description: "A detailed description",
			},
		},
		{
			name: "default dashboard",
			req: &api.CreateDashboardRequest{
				Name:      "Default Dashboard",
				IsDefault: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dashboard, err := store.CreateDashboard(ctx, tt.req)
			if tt.wantError {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if dashboard.ID == "" {
				t.Error("expected dashboard ID to be set")
			}
			if dashboard.Name != tt.req.Name {
				t.Errorf("expected name %q, got %q", tt.req.Name, dashboard.Name)
			}
			if dashboard.Description != tt.req.Description {
				t.Errorf("expected description %q, got %q", tt.req.Description, dashboard.Description)
			}
			if dashboard.IsDefault != tt.req.IsDefault {
				t.Errorf("expected IsDefault %v, got %v", tt.req.IsDefault, dashboard.IsDefault)
			}
			if dashboard.CreatedAt.IsZero() {
				t.Error("expected CreatedAt to be set")
			}
			if dashboard.UpdatedAt.IsZero() {
				t.Error("expected UpdatedAt to be set")
			}
		})
	}
}

func TestCreateDashboard_DefaultOverride(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create first default dashboard
	d1, err := store.CreateDashboard(ctx, &api.CreateDashboardRequest{
		Name:      "First Default",
		IsDefault: true,
	})
	if err != nil {
		t.Fatalf("failed to create first dashboard: %v", err)
	}
	if !d1.IsDefault {
		t.Error("first dashboard should be default")
	}

	// Create second default dashboard - should unset first
	d2, err := store.CreateDashboard(ctx, &api.CreateDashboardRequest{
		Name:      "Second Default",
		IsDefault: true,
	})
	if err != nil {
		t.Fatalf("failed to create second dashboard: %v", err)
	}
	if !d2.IsDefault {
		t.Error("second dashboard should be default")
	}

	// Verify first is no longer default
	d1Updated, err := store.GetDashboard(ctx, d1.ID)
	if err != nil {
		t.Fatalf("failed to get first dashboard: %v", err)
	}
	if d1Updated.IsDefault {
		t.Error("first dashboard should no longer be default")
	}
}

func TestGetDashboards(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// Initially empty
	dashboards, err := store.GetDashboards(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(dashboards) != 0 {
		t.Errorf("expected 0 dashboards, got %d", len(dashboards))
	}

	// Create some dashboards
	_, err = store.CreateDashboard(ctx, &api.CreateDashboardRequest{Name: "Dashboard 1"})
	if err != nil {
		t.Fatalf("failed to create dashboard 1: %v", err)
	}
	_, err = store.CreateDashboard(ctx, &api.CreateDashboardRequest{Name: "Dashboard 2"})
	if err != nil {
		t.Fatalf("failed to create dashboard 2: %v", err)
	}

	dashboards, err = store.GetDashboards(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(dashboards) != 2 {
		t.Errorf("expected 2 dashboards, got %d", len(dashboards))
	}
}

func TestGetDashboard(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create a dashboard
	created, err := store.CreateDashboard(ctx, &api.CreateDashboardRequest{
		Name:        "Test Dashboard",
		Description: "Test description",
	})
	if err != nil {
		t.Fatalf("failed to create dashboard: %v", err)
	}

	// Get it back
	dashboard, err := store.GetDashboard(ctx, created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dashboard == nil {
		t.Fatal("expected dashboard, got nil")
	}
	if dashboard.ID != created.ID {
		t.Errorf("expected ID %q, got %q", created.ID, dashboard.ID)
	}
	if dashboard.Name != "Test Dashboard" {
		t.Errorf("expected name 'Test Dashboard', got %q", dashboard.Name)
	}
	if dashboard.Description != "Test description" {
		t.Errorf("expected description 'Test description', got %q", dashboard.Description)
	}
}

func TestGetDashboard_NotFound(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	dashboard, err := store.GetDashboard(ctx, "nonexistent-id")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dashboard != nil {
		t.Error("expected nil for nonexistent dashboard")
	}
}

func TestGetDefaultDashboard(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// No default initially
	dashboard, err := store.GetDefaultDashboard(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dashboard != nil {
		t.Error("expected nil when no default dashboard exists")
	}

	// Create a default dashboard
	created, err := store.CreateDashboard(ctx, &api.CreateDashboardRequest{
		Name:      "Default Dashboard",
		IsDefault: true,
	})
	if err != nil {
		t.Fatalf("failed to create dashboard: %v", err)
	}

	// Get default
	dashboard, err = store.GetDefaultDashboard(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dashboard == nil {
		t.Fatal("expected dashboard, got nil")
	}
	if dashboard.Dashboard.ID != created.ID {
		t.Errorf("expected ID %q, got %q", created.ID, dashboard.Dashboard.ID)
	}
	if !dashboard.Dashboard.IsDefault {
		t.Error("expected IsDefault to be true")
	}
}

func TestSetDefaultDashboard(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create two dashboards
	d1, err := store.CreateDashboard(ctx, &api.CreateDashboardRequest{Name: "Dashboard 1"})
	if err != nil {
		t.Fatalf("failed to create dashboard 1: %v", err)
	}
	d2, err := store.CreateDashboard(ctx, &api.CreateDashboardRequest{Name: "Dashboard 2"})
	if err != nil {
		t.Fatalf("failed to create dashboard 2: %v", err)
	}

	// Set d1 as default
	if err := store.SetDefaultDashboard(ctx, d1.ID); err != nil {
		t.Fatalf("failed to set default: %v", err)
	}

	defaultDash, err := store.GetDefaultDashboard(ctx)
	if err != nil {
		t.Fatalf("failed to get default: %v", err)
	}
	if defaultDash.Dashboard.ID != d1.ID {
		t.Errorf("expected default to be d1, got %s", defaultDash.Dashboard.ID)
	}

	// Change default to d2
	if err := store.SetDefaultDashboard(ctx, d2.ID); err != nil {
		t.Fatalf("failed to set default: %v", err)
	}

	defaultDash, err = store.GetDefaultDashboard(ctx)
	if err != nil {
		t.Fatalf("failed to get default: %v", err)
	}
	if defaultDash.Dashboard.ID != d2.ID {
		t.Errorf("expected default to be d2, got %s", defaultDash.Dashboard.ID)
	}
}

func TestUpdateDashboard(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create a dashboard
	created, err := store.CreateDashboard(ctx, &api.CreateDashboardRequest{
		Name:        "Original Name",
		Description: "Original Description",
	})
	if err != nil {
		t.Fatalf("failed to create dashboard: %v", err)
	}

	// Update name
	updated, err := store.UpdateDashboard(ctx, created.ID, &api.UpdateDashboardRequest{
		Name: "Updated Name",
	})
	if err != nil {
		t.Fatalf("failed to update dashboard: %v", err)
	}
	if updated.Name != "Updated Name" {
		t.Errorf("expected name 'Updated Name', got %q", updated.Name)
	}

	// Update description
	updated, err = store.UpdateDashboard(ctx, created.ID, &api.UpdateDashboardRequest{
		Description: "Updated Description",
	})
	if err != nil {
		t.Fatalf("failed to update dashboard: %v", err)
	}
	if updated.Description != "Updated Description" {
		t.Errorf("expected description 'Updated Description', got %q", updated.Description)
	}
}

func TestDeleteDashboard(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	// Create a dashboard with a widget
	dashboard, err := store.CreateDashboard(ctx, &api.CreateDashboardRequest{Name: "To Delete"})
	if err != nil {
		t.Fatalf("failed to create dashboard: %v", err)
	}

	_, err = store.CreateWidget(ctx, dashboard.ID, &api.CreateWidgetRequest{
		WidgetType: "stats",
		Title:      "Test Widget",
	})
	if err != nil {
		t.Fatalf("failed to create widget: %v", err)
	}

	// Delete dashboard
	if err := store.DeleteDashboard(ctx, dashboard.ID); err != nil {
		t.Fatalf("failed to delete dashboard: %v", err)
	}

	// Verify gone
	deleted, err := store.GetDashboard(ctx, dashboard.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if deleted != nil {
		t.Error("expected dashboard to be deleted")
	}

	// Verify widgets also gone
	widgets, err := store.GetWidgetsForDashboard(ctx, dashboard.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(widgets) != 0 {
		t.Errorf("expected 0 widgets after delete, got %d", len(widgets))
	}
}

func TestCreateWidget(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	dashboard, err := store.CreateDashboard(ctx, &api.CreateDashboardRequest{Name: "Widget Dashboard"})
	if err != nil {
		t.Fatalf("failed to create dashboard: %v", err)
	}

	tests := []struct {
		name string
		req  *api.CreateWidgetRequest
	}{
		{
			name: "basic widget",
			req: &api.CreateWidgetRequest{
				WidgetType: "stats",
				Title:      "Basic Widget",
			},
		},
		{
			name: "widget with position",
			req: &api.CreateWidgetRequest{
				WidgetType: "metric_chart",
				Title:      "Positioned Widget",
				GridColumn: 2,
				GridRow:    1,
				ColSpan:    2,
				RowSpan:    1,
			},
		},
		{
			name: "widget with config",
			req: &api.CreateWidgetRequest{
				WidgetType: "metric_chart",
				Title:      "Config Widget",
				Config: api.WidgetConfig{
					Service:    "test-service",
					MetricName: "cpu.usage",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			widget, err := store.CreateWidget(ctx, dashboard.ID, tt.req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if widget.ID == "" {
				t.Error("expected widget ID to be set")
			}
			if widget.DashboardID != dashboard.ID {
				t.Errorf("expected dashboard ID %q, got %q", dashboard.ID, widget.DashboardID)
			}
			if widget.WidgetType != tt.req.WidgetType {
				t.Errorf("expected type %q, got %q", tt.req.WidgetType, widget.WidgetType)
			}
			if widget.Title != tt.req.Title {
				t.Errorf("expected title %q, got %q", tt.req.Title, widget.Title)
			}
			if widget.GridColumn != tt.req.GridColumn {
				t.Errorf("expected gridColumn %d, got %d", tt.req.GridColumn, widget.GridColumn)
			}
			if widget.GridRow != tt.req.GridRow {
				t.Errorf("expected gridRow %d, got %d", tt.req.GridRow, widget.GridRow)
			}
		})
	}
}

func TestCreateWidget_NonexistentDashboard(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	_, err := store.CreateWidget(ctx, "missing-dashboard", &api.CreateWidgetRequest{
		WidgetType: "stats",
		Title:      "Orphan Widget",
	})
	if !errors.Is(err, ErrDashboardNotFound) {
		t.Fatalf("expected ErrDashboardNotFound, got %v", err)
	}
}

func TestCreateWidget_DefaultSpans(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	dashboard, err := store.CreateDashboard(ctx, &api.CreateDashboardRequest{Name: "Widget Dashboard"})
	if err != nil {
		t.Fatalf("failed to create dashboard: %v", err)
	}

	widget, err := store.CreateWidget(ctx, dashboard.ID, &api.CreateWidgetRequest{
		WidgetType: "stats",
		Title:      "Default Span Widget",
		ColSpan:    0, // Should default to 1
		RowSpan:    0, // Should default to 1
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if widget.ColSpan != 1 {
		t.Errorf("expected colSpan 1 (default), got %d", widget.ColSpan)
	}
	if widget.RowSpan != 1 {
		t.Errorf("expected rowSpan 1 (default), got %d", widget.RowSpan)
	}
}

func TestGetWidgetsForDashboard(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	dashboard, err := store.CreateDashboard(ctx, &api.CreateDashboardRequest{Name: "Widget Dashboard"})
	if err != nil {
		t.Fatalf("failed to create dashboard: %v", err)
	}

	// Empty initially
	widgets, err := store.GetWidgetsForDashboard(ctx, dashboard.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(widgets) != 0 {
		t.Errorf("expected 0 widgets, got %d", len(widgets))
	}

	// Create widgets
	_, err = store.CreateWidget(ctx, dashboard.ID, &api.CreateWidgetRequest{
		WidgetType: "stats",
		Title:      "Widget 1",
		GridColumn: 0,
		GridRow:    0,
	})
	if err != nil {
		t.Fatalf("failed to create widget 1: %v", err)
	}
	_, err = store.CreateWidget(ctx, dashboard.ID, &api.CreateWidgetRequest{
		WidgetType: "metric_chart",
		Title:      "Widget 2",
		GridColumn: 1,
		GridRow:    0,
	})
	if err != nil {
		t.Fatalf("failed to create widget 2: %v", err)
	}

	widgets, err = store.GetWidgetsForDashboard(ctx, dashboard.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(widgets) != 2 {
		t.Errorf("expected 2 widgets, got %d", len(widgets))
	}
}

func TestGetDashboardWithWidgets_EmptyWidgetsReturnsEmptySlice(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	dashboard, err := store.CreateDashboard(ctx, &api.CreateDashboardRequest{Name: "Empty Dashboard"})
	if err != nil {
		t.Fatalf("failed to create dashboard: %v", err)
	}

	result, err := store.GetDashboardWithWidgets(ctx, dashboard.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected dashboard, got nil")
	}
	if result.Widgets == nil {
		t.Fatal("expected empty widget slice, got nil")
	}
	if len(result.Widgets) != 0 {
		t.Fatalf("expected 0 widgets, got %d", len(result.Widgets))
	}
}

func TestUpdateWidget(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	dashboard, err := store.CreateDashboard(ctx, &api.CreateDashboardRequest{Name: "Widget Dashboard"})
	if err != nil {
		t.Fatalf("failed to create dashboard: %v", err)
	}

	widget, err := store.CreateWidget(ctx, dashboard.ID, &api.CreateWidgetRequest{
		WidgetType: "stats",
		Title:      "Original Title",
		GridColumn: 0,
		GridRow:    0,
	})
	if err != nil {
		t.Fatalf("failed to create widget: %v", err)
	}

	// Update title
	updated, err := store.UpdateWidget(ctx, dashboard.ID, widget.ID, &api.UpdateWidgetRequest{
		Title: "Updated Title",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Title != "Updated Title" {
		t.Errorf("expected title 'Updated Title', got %q", updated.Title)
	}

	// Update position
	updated, err = store.UpdateWidget(ctx, dashboard.ID, widget.ID, &api.UpdateWidgetRequest{
		GridColumn: 2,
		GridRow:    3,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.GridColumn != 2 {
		t.Errorf("expected gridColumn 2, got %d", updated.GridColumn)
	}
	if updated.GridRow != 3 {
		t.Errorf("expected gridRow 3, got %d", updated.GridRow)
	}

	// Update config
	updated, err = store.UpdateWidget(ctx, dashboard.ID, widget.ID, &api.UpdateWidgetRequest{
		Config: api.WidgetConfig{
			Service:    "new-service",
			MetricName: "new-metric",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.Config.Service != "new-service" {
		t.Errorf("expected service 'new-service', got %q", updated.Config.Service)
	}
}

func TestUpdateWidget_WrongDashboard(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	dashboard, err := store.CreateDashboard(ctx, &api.CreateDashboardRequest{Name: "Widget Dashboard"})
	if err != nil {
		t.Fatalf("failed to create dashboard: %v", err)
	}
	otherDashboard, err := store.CreateDashboard(ctx, &api.CreateDashboardRequest{Name: "Other Dashboard"})
	if err != nil {
		t.Fatalf("failed to create other dashboard: %v", err)
	}

	widget, err := store.CreateWidget(ctx, dashboard.ID, &api.CreateWidgetRequest{
		WidgetType: "stats",
		Title:      "Original Title",
	})
	if err != nil {
		t.Fatalf("failed to create widget: %v", err)
	}

	_, err = store.UpdateWidget(ctx, otherDashboard.ID, widget.ID, &api.UpdateWidgetRequest{
		Title: "Wrong Dashboard",
	})
	if !errors.Is(err, ErrWidgetNotFound) {
		t.Fatalf("expected ErrWidgetNotFound, got %v", err)
	}
}

func TestUpdateWidgetPositions(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	dashboard, err := store.CreateDashboard(ctx, &api.CreateDashboardRequest{Name: "Widget Dashboard"})
	if err != nil {
		t.Fatalf("failed to create dashboard: %v", err)
	}

	w1, err := store.CreateWidget(ctx, dashboard.ID, &api.CreateWidgetRequest{
		WidgetType: "stats",
		Title:      "Widget 1",
		GridColumn: 0,
		GridRow:    0,
	})
	if err != nil {
		t.Fatalf("failed to create widget 1: %v", err)
	}

	w2, err := store.CreateWidget(ctx, dashboard.ID, &api.CreateWidgetRequest{
		WidgetType: "stats",
		Title:      "Widget 2",
		GridColumn: 1,
		GridRow:    0,
	})
	if err != nil {
		t.Fatalf("failed to create widget 2: %v", err)
	}

	// Update positions
	positions := []api.WidgetPosition{
		{ID: w1.ID, GridColumn: 2, GridRow: 1},
		{ID: w2.ID, GridColumn: 0, GridRow: 2},
	}

	if err := store.UpdateWidgetPositions(ctx, dashboard.ID, positions); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify positions
	widgets, err := store.GetWidgetsForDashboard(ctx, dashboard.ID)
	if err != nil {
		t.Fatalf("failed to get widgets: %v", err)
	}

	positionMap := make(map[string]api.DashboardWidget)
	for _, w := range widgets {
		positionMap[w.ID] = w
	}

	if positionMap[w1.ID].GridColumn != 2 || positionMap[w1.ID].GridRow != 1 {
		t.Errorf("w1 position incorrect: got (%d, %d), expected (2, 1)",
			positionMap[w1.ID].GridColumn, positionMap[w1.ID].GridRow)
	}
	if positionMap[w2.ID].GridColumn != 0 || positionMap[w2.ID].GridRow != 2 {
		t.Errorf("w2 position incorrect: got (%d, %d), expected (0, 2)",
			positionMap[w2.ID].GridColumn, positionMap[w2.ID].GridRow)
	}
}

func TestUpdateWidgetPositions_NotFound(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	dashboard, err := store.CreateDashboard(ctx, &api.CreateDashboardRequest{Name: "Widget Dashboard"})
	if err != nil {
		t.Fatalf("failed to create dashboard: %v", err)
	}

	err = store.UpdateWidgetPositions(ctx, dashboard.ID, []api.WidgetPosition{
		{ID: "missing-widget", GridColumn: 1, GridRow: 1},
	})
	if !errors.Is(err, ErrWidgetNotFound) {
		t.Fatalf("expected ErrWidgetNotFound, got %v", err)
	}
}

func TestUpdateWidgetPositions_NonexistentDashboard(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	err := store.UpdateWidgetPositions(ctx, "missing-dashboard", []api.WidgetPosition{})
	if !errors.Is(err, ErrDashboardNotFound) {
		t.Fatalf("expected ErrDashboardNotFound, got %v", err)
	}
}

func TestUpdateWidgetPositions_Empty(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	dashboard, err := store.CreateDashboard(ctx, &api.CreateDashboardRequest{Name: "Widget Dashboard"})
	if err != nil {
		t.Fatalf("failed to create dashboard: %v", err)
	}

	// Empty positions should succeed
	if err := store.UpdateWidgetPositions(ctx, dashboard.ID, []api.WidgetPosition{}); err != nil {
		t.Fatalf("unexpected error for empty positions: %v", err)
	}
}

func TestDeleteWidget(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	dashboard, err := store.CreateDashboard(ctx, &api.CreateDashboardRequest{Name: "Widget Dashboard"})
	if err != nil {
		t.Fatalf("failed to create dashboard: %v", err)
	}

	widget, err := store.CreateWidget(ctx, dashboard.ID, &api.CreateWidgetRequest{
		WidgetType: "stats",
		Title:      "To Delete",
	})
	if err != nil {
		t.Fatalf("failed to create widget: %v", err)
	}

	if err := store.DeleteWidget(ctx, dashboard.ID, widget.ID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	widgets, err := store.GetWidgetsForDashboard(ctx, dashboard.ID)
	if err != nil {
		t.Fatalf("failed to get widgets: %v", err)
	}
	if len(widgets) != 0 {
		t.Errorf("expected 0 widgets after delete, got %d", len(widgets))
	}
}

func TestDashboardWidgetsPersistAfterReopen(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "dashboards.duckdb")

	store, err := NewDuckDBStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	dashboard, err := store.CreateDashboard(ctx, &api.CreateDashboardRequest{
		Name:        "Persistent Dashboard",
		Description: "Survives reopen",
		IsDefault:   true,
	})
	if err != nil {
		t.Fatalf("failed to create dashboard: %v", err)
	}

	createdWidget, err := store.CreateWidget(ctx, dashboard.ID, &api.CreateWidgetRequest{
		WidgetType: "metric_chart",
		Title:      "Persistent Widget",
		GridColumn: 1,
		GridRow:    2,
		ColSpan:    2,
		RowSpan:    1,
		Config: api.WidgetConfig{
			Service:    "copilot-chat",
			MetricName: "github_copilot.token.usage",
		},
	})
	if err != nil {
		t.Fatalf("failed to create widget: %v", err)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("failed to close store: %v", err)
	}

	reopened, err := NewDuckDBStore(dbPath)
	if err != nil {
		t.Fatalf("failed to reopen store: %v", err)
	}
	defer reopened.Close()

	result, err := reopened.GetDashboardWithWidgets(ctx, dashboard.ID)
	if err != nil {
		t.Fatalf("failed to load dashboard after reopen: %v", err)
	}
	if result == nil {
		t.Fatal("expected dashboard after reopen, got nil")
	}
	if len(result.Widgets) != 1 {
		t.Fatalf("expected 1 widget after reopen, got %d", len(result.Widgets))
	}

	widget := result.Widgets[0]
	if widget.ID != createdWidget.ID {
		t.Errorf("expected widget ID %q, got %q", createdWidget.ID, widget.ID)
	}
	if widget.DashboardID != dashboard.ID {
		t.Errorf("expected dashboard ID %q, got %q", dashboard.ID, widget.DashboardID)
	}
	if widget.Config.Service != "copilot-chat" {
		t.Errorf("expected service copilot-chat, got %q", widget.Config.Service)
	}
	if widget.Config.MetricName != "github_copilot.token.usage" {
		t.Errorf("expected metric github_copilot.token.usage, got %q", widget.Config.MetricName)
	}
}

func TestGetDashboardWithWidgets(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	dashboard, err := store.CreateDashboard(ctx, &api.CreateDashboardRequest{
		Name:        "Full Dashboard",
		Description: "With widgets",
	})
	if err != nil {
		t.Fatalf("failed to create dashboard: %v", err)
	}

	// Create widgets
	_, err = store.CreateWidget(ctx, dashboard.ID, &api.CreateWidgetRequest{
		WidgetType: "stats",
		Title:      "Widget 1",
	})
	if err != nil {
		t.Fatalf("failed to create widget 1: %v", err)
	}
	_, err = store.CreateWidget(ctx, dashboard.ID, &api.CreateWidgetRequest{
		WidgetType: "metric_chart",
		Title:      "Widget 2",
	})
	if err != nil {
		t.Fatalf("failed to create widget 2: %v", err)
	}

	// Get dashboard with widgets
	result, err := store.GetDashboardWithWidgets(ctx, dashboard.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected result, got nil")
	}
	if result.Dashboard.ID != dashboard.ID {
		t.Errorf("expected dashboard ID %q, got %q", dashboard.ID, result.Dashboard.ID)
	}
	if result.Dashboard.Name != "Full Dashboard" {
		t.Errorf("expected name 'Full Dashboard', got %q", result.Dashboard.Name)
	}
	if len(result.Widgets) != 2 {
		t.Errorf("expected 2 widgets, got %d", len(result.Widgets))
	}
}

func TestGetDashboardWithWidgets_NotFound(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx := context.Background()

	result, err := store.GetDashboardWithWidgets(ctx, "nonexistent-id")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Error("expected nil for nonexistent dashboard")
	}
}

func TestScanWidgetConfig(t *testing.T) {
	tests := []struct {
		name   string
		input  interface{}
		expect api.WidgetConfig
	}{
		{
			name:   "nil input",
			input:  nil,
			expect: api.WidgetConfig{},
		},
		{
			name:   "empty string",
			input:  "",
			expect: api.WidgetConfig{},
		},
		{
			name:   "empty JSON object",
			input:  "{}",
			expect: api.WidgetConfig{},
		},
		{
			name:  "valid JSON string",
			input: `{"service":"test-svc","metricName":"cpu"}`,
			expect: api.WidgetConfig{
				Service:    "test-svc",
				MetricName: "cpu",
			},
		},
		{
			name:  "map input",
			input: map[string]interface{}{"service": "map-svc", "metricName": "memory"},
			expect: api.WidgetConfig{
				Service:    "map-svc",
				MetricName: "memory",
			},
		},
		{
			name:   "invalid JSON string",
			input:  "not valid json",
			expect: api.WidgetConfig{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := scanWidgetConfig(tt.input)
			if result.Service != tt.expect.Service {
				t.Errorf("expected service %q, got %q", tt.expect.Service, result.Service)
			}
			if result.MetricName != tt.expect.MetricName {
				t.Errorf("expected metricName %q, got %q", tt.expect.MetricName, result.MetricName)
			}
		})
	}
}

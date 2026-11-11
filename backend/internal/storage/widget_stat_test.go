package storage

import (
	"context"
	"testing"

	"github.com/tobilg/ai-observer/internal/api"
)

// A metric widget keeps the statistic of its histogram metric
func TestWidgetConfigKeepsStat(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()
	dashboard, err := store.CreateDashboard(ctx, &api.CreateDashboardRequest{Name: "d"})
	if err != nil {
		t.Fatal(err)
	}
	created, err := store.CreateWidget(ctx, dashboard.ID, &api.CreateWidgetRequest{
		WidgetType: "metric_chart", Title: "Latency", GridColumn: 1, GridRow: 1, ColSpan: 2, RowSpan: 1,
		Config: api.WidgetConfig{MetricName: "gemini_cli.api.request.latency", Stat: "p95"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Config.Stat != "p95" {
		t.Errorf("created config = %+v", created.Config)
	}
	stored, err := store.GetDashboardWithWidgets(ctx, dashboard.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Widgets) != 1 || stored.Widgets[0].Config.Stat != "p95" {
		t.Fatalf("stored widgets = %+v", stored.Widgets)
	}

	updated, err := store.UpdateWidget(ctx, dashboard.ID, created.ID, &api.UpdateWidgetRequest{
		Config: &api.WidgetConfig{MetricName: "gemini_cli.api.request.latency", Stat: "max"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Config.Stat != "max" {
		t.Errorf("updated config = %+v", updated.Config)
	}
	stored, err = store.GetDashboardWithWidgets(ctx, dashboard.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Widgets[0].Config.Stat != "max" {
		t.Errorf("stored after update = %+v", stored.Widgets[0].Config)
	}
}

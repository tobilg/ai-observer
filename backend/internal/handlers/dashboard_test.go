package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/tobilg/ai-observer/internal/api"
)

func TestListDashboards(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/dashboards", nil)
	rec := httptest.NewRecorder()

	h.ListDashboards(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	var resp api.DashboardsResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Empty database should have no dashboards
	if len(resp.Dashboards) != 0 {
		t.Errorf("expected 0 dashboards, got %d", len(resp.Dashboards))
	}
}

func TestCreateDashboard(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()

	tests := []struct {
		name       string
		body       interface{}
		wantStatus int
	}{
		{
			name:       "valid request",
			body:       map[string]interface{}{"name": "Test Dashboard"},
			wantStatus: http.StatusCreated,
		},
		{
			name:       "missing name",
			body:       map[string]interface{}{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "empty name",
			body:       map[string]interface{}{"name": ""},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid json",
			body:       "invalid",
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body []byte
			if s, ok := tt.body.(string); ok {
				body = []byte(s)
			} else {
				body, _ = json.Marshal(tt.body)
			}

			req := httptest.NewRequest(http.MethodPost, "/api/dashboards", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			h.CreateDashboard(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("expected status %d, got %d: %s", tt.wantStatus, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestGetDashboard(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()

	// Create a dashboard first
	dashboard := createTestDashboard(t, h, "Test Dashboard")

	tests := []struct {
		name       string
		id         string
		wantStatus int
	}{
		{"existing dashboard", dashboard.ID, http.StatusOK},
		{"non-existent dashboard", "nonexistent-id", http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/dashboards/"+tt.id, nil)
			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("id", tt.id)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			rec := httptest.NewRecorder()
			h.GetDashboard(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("expected status %d, got %d: %s", tt.wantStatus, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestGetDashboard_MissingID(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/dashboards/", nil)
	rctx := chi.NewRouteContext()
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	h.GetDashboard(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rec.Code)
	}
}

func TestGetDefaultDashboard(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()

	get := func() api.DashboardWithWidgets {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/dashboards/default", nil)
		rec := httptest.NewRecorder()
		h.GetDefaultDashboard(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var d api.DashboardWithWidgets
		if err := json.NewDecoder(rec.Body).Decode(&d); err != nil {
			t.Fatal(err)
		}
		return d
	}

	// Without a default, the server creates it with the default widgets
	created := get()
	if created.Name != "Default Dashboard" || created.Description != "AI Observer default dashboard" || !created.IsDefault {
		t.Errorf("created dashboard = %+v", created.Dashboard)
	}
	want := []string{
		"stats_traces Total Traces 1,1 1x1", "stats_metrics Metrics 2,1 1x1", "stats_logs Logs 3,1 1x1",
		"stats_error_rate Error Rate 4,1 1x1", "active_services Active Services 1,2 2x1", "recent_activity Recent Traces 1,3 4x1",
	}
	if len(created.Widgets) != len(want) {
		t.Fatalf("widgets = %+v", created.Widgets)
	}
	for i, w := range created.Widgets {
		got := fmt.Sprintf("%s %s %d,%d %dx%d", w.WidgetType, w.Title, w.GridColumn, w.GridRow, w.ColSpan, w.RowSpan)
		if got != want[i] || w.DashboardID != created.ID {
			t.Errorf("widget %d = %s (dashboard %s), want %s", i, got, w.DashboardID, want[i])
		}
	}
	if again := get(); again.ID != created.ID || len(again.Widgets) != len(want) {
		t.Errorf("second request = %s with %d widgets, want the created dashboard", again.ID, len(again.Widgets))
	}

	// Another default dashboard is returned as it is
	dashboard := createTestDashboard(t, h, "Mine")
	setDashboardAsDefault(t, h, dashboard.ID)
	if d := get(); d.ID != dashboard.ID || len(d.Widgets) != 0 {
		t.Errorf("default = %s with %d widgets, want %s without widgets", d.ID, len(d.Widgets), dashboard.ID)
	}
}

// Two dashboard tabs opening at once create one default dashboard
func TestGetDefaultDashboard_Concurrent(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()

	const requests = 8
	ids := make(chan string, requests)
	var wg sync.WaitGroup
	for range requests {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := httptest.NewRecorder()
			h.GetDefaultDashboard(rec, httptest.NewRequest(http.MethodGet, "/api/dashboards/default", nil))
			var d api.DashboardWithWidgets
			if rec.Code != http.StatusOK || json.NewDecoder(rec.Body).Decode(&d) != nil || len(d.Widgets) != 6 {
				t.Errorf("status %d: %s", rec.Code, rec.Body.String())
			}
			ids <- d.ID
		}()
	}
	wg.Wait()
	close(ids)
	seen := make(map[string]bool)
	for id := range ids {
		seen[id] = true
	}
	dashboards, err := h.store.GetDashboards(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 || len(dashboards) != 1 {
		t.Errorf("requests returned %d dashboards, %d stored; want one", len(seen), len(dashboards))
	}
}

func TestUpdateDashboard(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()

	dashboard := createTestDashboard(t, h, "Original Name")

	tests := []struct {
		name       string
		id         string
		body       interface{}
		wantStatus int
	}{
		{
			name:       "valid update",
			id:         dashboard.ID,
			body:       map[string]interface{}{"name": "Updated Name"},
			wantStatus: http.StatusOK,
		},
		{
			name:       "invalid json",
			id:         dashboard.ID,
			body:       "invalid",
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var body []byte
			if s, ok := tt.body.(string); ok {
				body = []byte(s)
			} else {
				body, _ = json.Marshal(tt.body)
			}

			req := httptest.NewRequest(http.MethodPut, "/api/dashboards/"+tt.id, bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("id", tt.id)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			rec := httptest.NewRecorder()
			h.UpdateDashboard(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("expected status %d, got %d: %s", tt.wantStatus, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestDeleteDashboard(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()

	dashboard := createTestDashboard(t, h, "To Delete")

	req := httptest.NewRequest(http.MethodDelete, "/api/dashboards/"+dashboard.ID, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", dashboard.ID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	h.DeleteDashboard(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("expected status 204, got %d", rec.Code)
	}
}

func TestCreateWidget(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()

	dashboard := createTestDashboard(t, h, "Widget Dashboard")

	tests := []struct {
		name       string
		body       interface{}
		wantStatus int
	}{
		{
			name: "valid widget",
			body: map[string]interface{}{
				"widgetType": "metric_chart",
				"title":      "CPU Usage",
				"gridColumn": 0,
				"gridRow":    0,
				"colSpan":    2,
				"rowSpan":    1,
			},
			wantStatus: http.StatusCreated,
		},
		{
			name:       "missing widgetType",
			body:       map[string]interface{}{"title": "No Type"},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "colSpan wider than the grid",
			body:       map[string]interface{}{"widgetType": "stats", "title": "Wide", "colSpan": 5},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing title",
			body:       map[string]interface{}{"widgetType": "stats"},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "negative gridColumn",
			body: map[string]interface{}{
				"widgetType": "stats",
				"title":      "Test",
				"gridColumn": -1,
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "negative gridRow",
			body: map[string]interface{}{
				"widgetType": "stats",
				"title":      "Test",
				"gridRow":    -1,
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "negative colSpan",
			body: map[string]interface{}{
				"widgetType": "stats",
				"title":      "Test",
				"colSpan":    -1,
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "negative rowSpan",
			body: map[string]interface{}{
				"widgetType": "stats",
				"title":      "Test",
				"rowSpan":    -1,
			},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.body)
			req := httptest.NewRequest(http.MethodPost, "/api/dashboards/"+dashboard.ID+"/widgets", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("id", dashboard.ID)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			rec := httptest.NewRecorder()
			h.CreateWidget(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("expected status %d, got %d: %s", tt.wantStatus, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestCreateWidget_DashboardNotFound(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()

	body, _ := json.Marshal(map[string]interface{}{
		"widgetType": "stats",
		"title":      "Missing Dashboard Widget",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/dashboards/missing-dashboard/widgets", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "missing-dashboard")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	h.CreateWidget(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestDeleteWidget(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()

	dashboard := createTestDashboard(t, h, "Widget Dashboard")
	widget := createTestWidget(t, h, dashboard.ID, "Test Widget")

	req := httptest.NewRequest(http.MethodDelete, "/api/dashboards/"+dashboard.ID+"/widgets/"+widget.ID, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", dashboard.ID)
	rctx.URLParams.Add("widgetId", widget.ID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	h.DeleteWidget(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("expected status 204, got %d", rec.Code)
	}
}

func TestDeleteWidget_NotFound(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()

	dashboard := createTestDashboard(t, h, "Widget Dashboard")

	req := httptest.NewRequest(http.MethodDelete, "/api/dashboards/"+dashboard.ID+"/widgets/missing-widget", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", dashboard.ID)
	rctx.URLParams.Add("widgetId", "missing-widget")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	h.DeleteWidget(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUpdateWidgetPositions(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()

	dashboard := createTestDashboard(t, h, "Widget Dashboard")
	widget := createTestWidget(t, h, dashboard.ID, "Test Widget")

	tests := []struct {
		name       string
		body       interface{}
		wantStatus int
	}{
		{
			name: "valid positions",
			body: map[string]interface{}{
				"positions": []map[string]interface{}{
					{"id": widget.ID, "gridColumn": 1, "gridRow": 1, "colSpan": 2, "rowSpan": 1},
				},
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "negative gridColumn",
			body: map[string]interface{}{
				"positions": []map[string]interface{}{
					{"id": widget.ID, "gridColumn": -1, "gridRow": 0},
				},
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "negative gridRow",
			body: map[string]interface{}{
				"positions": []map[string]interface{}{
					{"id": widget.ID, "gridColumn": 0, "gridRow": -1},
				},
			},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.body)
			req := httptest.NewRequest(http.MethodPut, "/api/dashboards/"+dashboard.ID+"/widgets/positions", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("id", dashboard.ID)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			rec := httptest.NewRecorder()
			h.UpdateWidgetPositions(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("expected status %d, got %d: %s", tt.wantStatus, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestUpdateWidgetPositions_NotFound(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()

	dashboard := createTestDashboard(t, h, "Widget Dashboard")

	body, _ := json.Marshal(map[string]interface{}{
		"positions": []map[string]interface{}{
			{"id": "missing-widget", "gridColumn": 1, "gridRow": 1},
		},
	})
	req := httptest.NewRequest(http.MethodPut, "/api/dashboards/"+dashboard.ID+"/widgets/positions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", dashboard.ID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	h.UpdateWidgetPositions(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUpdateWidget_NotFound(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()

	dashboard := createTestDashboard(t, h, "Widget Dashboard")

	body, _ := json.Marshal(map[string]interface{}{"title": "Updated"})
	req := httptest.NewRequest(http.MethodPut, "/api/dashboards/"+dashboard.ID+"/widgets/missing-widget", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", dashboard.ID)
	rctx.URLParams.Add("widgetId", "missing-widget")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	h.UpdateWidget(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

// Helper functions for dashboard tests

func createTestDashboard(t *testing.T, h *Handlers, name string) *api.Dashboard {
	t.Helper()
	body, _ := json.Marshal(map[string]interface{}{"name": name})
	req := httptest.NewRequest(http.MethodPost, "/api/dashboards", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.CreateDashboard(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("failed to create test dashboard: %d %s", rec.Code, rec.Body.String())
	}

	var dashboard api.Dashboard
	if err := json.NewDecoder(rec.Body).Decode(&dashboard); err != nil {
		t.Fatalf("failed to decode dashboard: %v", err)
	}
	return &dashboard
}

func createTestWidget(t *testing.T, h *Handlers, dashboardID, title string) *api.DashboardWidget {
	t.Helper()
	body, _ := json.Marshal(map[string]interface{}{
		"widgetType": "stats",
		"title":      title,
		"gridColumn": 0,
		"gridRow":    0,
		"colSpan":    1,
		"rowSpan":    1,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/dashboards/"+dashboardID+"/widgets", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", dashboardID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()

	h.CreateWidget(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("failed to create test widget: %d %s", rec.Code, rec.Body.String())
	}

	var widget api.DashboardWidget
	if err := json.NewDecoder(rec.Body).Decode(&widget); err != nil {
		t.Fatalf("failed to decode widget: %v", err)
	}
	return &widget
}

func setDashboardAsDefault(t *testing.T, h *Handlers, id string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, "/api/dashboards/"+id+"/default", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()

	h.SetDefaultDashboard(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("failed to set default dashboard: %d %s", rec.Code, rec.Body.String())
	}
}

// updateWidget sends PUT /api/dashboards/{id}/widgets/{widgetId} with body
func updateWidget(t *testing.T, h *Handlers, dashboardID, widgetID, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, "/api/dashboards/"+dashboardID+"/widgets/"+widgetID, bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", dashboardID)
	rctx.URLParams.Add("widgetId", widgetID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	h.UpdateWidget(rec, req)
	return rec
}

func TestUpdateWidget(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()

	dashboard := createTestDashboard(t, h, "Widget Dashboard")
	widget := createTestWidget(t, h, dashboard.ID, "Tokens")
	decode := func(rec *httptest.ResponseRecorder) api.DashboardWidget {
		t.Helper()
		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var w api.DashboardWidget
		if err := json.NewDecoder(rec.Body).Decode(&w); err != nil {
			t.Fatal(err)
		}
		return w
	}

	// Title, config and spans together; the response is the updated widget
	w := decode(updateWidget(t, h, dashboard.ID, widget.ID, `{"title": "Token usage", "colSpan": 2, "rowSpan": 3,
		"config": {"metricName": "claude_code.token.usage", "service": "claude-code", "breakdownAttribute": "type", "chartStacked": false}}`))
	if w.ID != widget.ID || w.Title != "Token usage" || w.ColSpan != 2 || w.RowSpan != 3 || w.WidgetType != "stats" {
		t.Errorf("updated widget = %+v", w)
	}
	if w.Config.MetricName != "claude_code.token.usage" || w.Config.Service != "claude-code" ||
		w.Config.BreakdownAttribute != "type" || w.Config.ChartStacked == nil || *w.Config.ChartStacked {
		t.Errorf("updated config = %+v", w.Config)
	}
	if !w.UpdatedAt.After(widget.UpdatedAt) && !w.UpdatedAt.Equal(widget.UpdatedAt) {
		t.Errorf("updatedAt %v before %v", w.UpdatedAt, widget.UpdatedAt)
	}

	// Fields left out are kept, the config too
	w = decode(updateWidget(t, h, dashboard.ID, widget.ID, `{"title": "Renamed"}`))
	if w.Title != "Renamed" || w.ColSpan != 2 || w.RowSpan != 3 || w.Config.MetricName != "claude_code.token.usage" {
		t.Errorf("after title update: %+v", w)
	}
	w = decode(updateWidget(t, h, dashboard.ID, widget.ID, `{"colSpan": 4}`))
	if w.Title != "Renamed" || w.ColSpan != 4 || w.RowSpan != 3 || w.Config.Service != "claude-code" {
		t.Errorf("after span update: %+v", w)
	}

	// A config replaces the stored one whole; {} clears it
	w = decode(updateWidget(t, h, dashboard.ID, widget.ID, `{"config": {"metricName": "other"}}`))
	if w.Config.MetricName != "other" || w.Config.Service != "" || w.Config.ChartStacked != nil {
		t.Errorf("after config update: %+v", w.Config)
	}
	w = decode(updateWidget(t, h, dashboard.ID, widget.ID, `{"config": {}}`))
	if w.Config != (api.WidgetConfig{}) {
		t.Errorf("after clearing config: %+v", w.Config)
	}

	// What the dashboard returns matches
	stored, err := h.store.GetDashboardWithWidgets(context.Background(), dashboard.ID)
	if err != nil || len(stored.Widgets) != 1 || stored.Widgets[0].Title != "Renamed" || stored.Widgets[0].ColSpan != 4 {
		t.Errorf("stored widgets = %+v, %v", stored, err)
	}

	for body, want := range map[string]int{
		`{"colSpan": 5}`:                 http.StatusBadRequest,
		`{"colSpan": -1}`:                http.StatusBadRequest,
		`{"rowSpan": -1}`:                http.StatusBadRequest,
		`{"title": "` + longTitle + `"}`: http.StatusBadRequest,
		`not json`:                       http.StatusBadRequest,
	} {
		if rec := updateWidget(t, h, dashboard.ID, widget.ID, body); rec.Code != want {
			t.Errorf("%s: status %d, want %d", body, rec.Code, want)
		}
	}
	// A widget of another dashboard
	other := createTestDashboard(t, h, "Other")
	if rec := updateWidget(t, h, other.ID, widget.ID, `{"title": "x"}`); rec.Code != http.StatusNotFound {
		t.Errorf("widget of another dashboard: status %d, want 404", rec.Code)
	}
}

var longTitle = string(bytes.Repeat([]byte("t"), 256))

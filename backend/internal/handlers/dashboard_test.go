package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

	// No default dashboard initially
	req := httptest.NewRequest(http.MethodGet, "/api/dashboards/default", nil)
	rec := httptest.NewRecorder()
	h.GetDefaultDashboard(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected status 404 for no default dashboard, got %d", rec.Code)
	}

	// Create a dashboard and set it as default
	dashboard := createTestDashboard(t, h, "Default Dashboard")
	setDashboardAsDefault(t, h, dashboard.ID)

	// Now should return the dashboard
	req = httptest.NewRequest(http.MethodGet, "/api/dashboards/default", nil)
	rec = httptest.NewRecorder()
	h.GetDefaultDashboard(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200 after setting default, got %d: %s", rec.Code, rec.Body.String())
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

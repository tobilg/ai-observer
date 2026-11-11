package handlers

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/storage"
)

// ListDashboards handles GET /api/dashboards
func (h *Handlers) ListDashboards(w http.ResponseWriter, r *http.Request) {
	dashboards, err := h.store.GetDashboards(r.Context())
	if err != nil {
		api.WriteInternalError(w, "failed to list dashboards", err)
		return
	}

	if dashboards == nil {
		dashboards = []api.Dashboard{}
	}

	api.WriteJSON(w, http.StatusOK, api.DashboardsResponse{Dashboards: dashboards})
}

// CreateDashboard handles POST /api/dashboards
func (h *Handlers) CreateDashboard(w http.ResponseWriter, r *http.Request) {
	var req api.CreateDashboardRequest
	if !decodeBody(w, r, &req) {
		return
	}

	if req.Name == "" {
		api.WriteError(w, http.StatusBadRequest, "name is required")
		return
	}

	// Validate name length (max 255 characters)
	if len(req.Name) > 255 {
		api.WriteError(w, http.StatusBadRequest, "name must be at most 255 characters")
		return
	}

	dashboard, err := h.store.CreateDashboard(r.Context(), &req)
	if err != nil {
		api.WriteInternalError(w, "failed to create dashboard", err)
		return
	}

	api.WriteJSON(w, http.StatusCreated, dashboard)
}

// GetDefaultDashboard handles GET /api/dashboards/default. When there is no
// default dashboard (a new database, or the default was deleted), it
// creates one with the default widgets (storage.GetOrCreateDefaultDashboard),
// so concurrent first requests share one dashboard.
func (h *Handlers) GetDefaultDashboard(w http.ResponseWriter, r *http.Request) {
	dashboard, _, err := h.store.GetOrCreateDefaultDashboard(r.Context())
	if err != nil {
		api.WriteInternalError(w, "failed to load default dashboard", err)
		return
	}

	if dashboard == nil {
		api.WriteError(w, http.StatusNotFound, "no default dashboard found")
		return
	}

	api.WriteJSON(w, http.StatusOK, dashboard)
}

// GetDashboard handles GET /api/dashboards/{id}
func (h *Handlers) GetDashboard(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		api.WriteError(w, http.StatusBadRequest, "id is required")
		return
	}

	dashboard, err := h.store.GetDashboardWithWidgets(r.Context(), id)
	if err != nil {
		api.WriteInternalError(w, "failed to load dashboard", err)
		return
	}

	if dashboard == nil {
		api.WriteError(w, http.StatusNotFound, "dashboard not found")
		return
	}

	api.WriteJSON(w, http.StatusOK, dashboard)
}

// UpdateDashboard handles PUT /api/dashboards/{id}
func (h *Handlers) UpdateDashboard(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		api.WriteError(w, http.StatusBadRequest, "id is required")
		return
	}

	var req api.UpdateDashboardRequest
	if !decodeBody(w, r, &req) {
		return
	}

	// Validate name length if provided (max 255 characters)
	if len(req.Name) > 255 {
		api.WriteError(w, http.StatusBadRequest, "name must be at most 255 characters")
		return
	}

	dashboard, err := h.store.UpdateDashboard(r.Context(), id, &req)
	if err != nil {
		if errors.Is(err, storage.ErrDashboardNotFound) {
			api.WriteError(w, http.StatusNotFound, "dashboard not found")
			return
		}
		api.WriteInternalError(w, "failed to update dashboard", err)
		return
	}

	api.WriteJSON(w, http.StatusOK, dashboard)
}

// DeleteDashboard handles DELETE /api/dashboards/{id}
func (h *Handlers) DeleteDashboard(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		api.WriteError(w, http.StatusBadRequest, "id is required")
		return
	}

	if err := h.store.DeleteDashboard(r.Context(), id); err != nil {
		api.WriteInternalError(w, "failed to delete dashboard", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// SetDefaultDashboard handles PUT /api/dashboards/{id}/default
func (h *Handlers) SetDefaultDashboard(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		api.WriteError(w, http.StatusBadRequest, "id is required")
		return
	}

	if err := h.store.SetDefaultDashboard(r.Context(), id); err != nil {
		if errors.Is(err, storage.ErrDashboardNotFound) {
			api.WriteError(w, http.StatusNotFound, "dashboard not found")
			return
		}
		api.WriteInternalError(w, "failed to set default dashboard", err)
		return
	}

	api.WriteJSON(w, http.StatusOK, api.OKResponse{Status: "ok"})
}

// CreateWidget handles POST /api/dashboards/{id}/widgets
func (h *Handlers) CreateWidget(w http.ResponseWriter, r *http.Request) {
	dashboardID := chi.URLParam(r, "id")
	if dashboardID == "" {
		api.WriteError(w, http.StatusBadRequest, "dashboard id is required")
		return
	}

	var req api.CreateWidgetRequest
	if !decodeBody(w, r, &req) {
		return
	}

	if req.WidgetType == "" {
		api.WriteError(w, http.StatusBadRequest, "widgetType is required")
		return
	}
	if req.Title == "" {
		api.WriteError(w, http.StatusBadRequest, "title is required")
		return
	}

	// Validate title length (max 255 characters)
	if len(req.Title) > 255 {
		api.WriteError(w, http.StatusBadRequest, "title must be at most 255 characters")
		return
	}

	// Validate grid position
	if req.GridColumn < 0 {
		api.WriteError(w, http.StatusBadRequest, "gridColumn must be non-negative")
		return
	}
	if req.GridRow < 0 {
		api.WriteError(w, http.StatusBadRequest, "gridRow must be non-negative")
		return
	}
	if !validSpans(w, req.ColSpan, req.RowSpan) {
		return
	}

	widget, err := h.store.CreateWidget(r.Context(), dashboardID, &req)
	if err != nil {
		if errors.Is(err, storage.ErrDashboardNotFound) {
			api.WriteError(w, http.StatusNotFound, "dashboard not found")
			return
		}
		api.WriteInternalError(w, "failed to create widget", err)
		return
	}

	api.WriteJSON(w, http.StatusCreated, widget)
}

// UpdateWidgetPositions handles PUT /api/dashboards/{id}/widgets/positions
func (h *Handlers) UpdateWidgetPositions(w http.ResponseWriter, r *http.Request) {
	dashboardID := chi.URLParam(r, "id")
	if dashboardID == "" {
		api.WriteError(w, http.StatusBadRequest, "dashboard id is required")
		return
	}

	var req api.UpdateWidgetPositionsRequest
	if !decodeBody(w, r, &req) {
		return
	}

	// Validate all positions
	for _, pos := range req.Positions {
		if pos.GridColumn < 0 {
			api.WriteError(w, http.StatusBadRequest, "gridColumn must be non-negative")
			return
		}
		if pos.GridRow < 0 {
			api.WriteError(w, http.StatusBadRequest, "gridRow must be non-negative")
			return
		}
	}

	if err := h.store.UpdateWidgetPositions(r.Context(), dashboardID, req.Positions); err != nil {
		if errors.Is(err, storage.ErrDashboardNotFound) {
			api.WriteError(w, http.StatusNotFound, "dashboard not found")
			return
		}
		if errors.Is(err, storage.ErrWidgetNotFound) {
			api.WriteError(w, http.StatusNotFound, "widget not found")
			return
		}
		api.WriteInternalError(w, "failed to update widget positions", err)
		return
	}

	api.WriteJSON(w, http.StatusOK, api.OKResponse{Status: "ok"})
}

// UpdateWidget handles PUT /api/dashboards/{id}/widgets/{widgetId}
func (h *Handlers) UpdateWidget(w http.ResponseWriter, r *http.Request) {
	dashboardID := chi.URLParam(r, "id")
	widgetID := chi.URLParam(r, "widgetId")
	if dashboardID == "" || widgetID == "" {
		api.WriteError(w, http.StatusBadRequest, "dashboard id and widget id are required")
		return
	}

	var req api.UpdateWidgetRequest
	if !decodeBody(w, r, &req) {
		return
	}

	// Validate title length if provided (max 255 characters)
	if len(req.Title) > 255 {
		api.WriteError(w, http.StatusBadRequest, "title must be at most 255 characters")
		return
	}

	// Validate grid position if provided
	if req.GridColumn < 0 {
		api.WriteError(w, http.StatusBadRequest, "gridColumn must be non-negative")
		return
	}
	if req.GridRow < 0 {
		api.WriteError(w, http.StatusBadRequest, "gridRow must be non-negative")
		return
	}
	if !validSpans(w, req.ColSpan, req.RowSpan) {
		return
	}

	widget, err := h.store.UpdateWidget(r.Context(), dashboardID, widgetID, &req)
	if err != nil {
		if errors.Is(err, storage.ErrWidgetNotFound) {
			api.WriteError(w, http.StatusNotFound, "widget not found")
			return
		}
		api.WriteInternalError(w, "failed to update widget", err)
		return
	}

	api.WriteJSON(w, http.StatusOK, widget)
}

// validSpans checks the spans of a widget request (0: unset), answering 400
// and returning false for invalid ones. A widget spans at most the grid's
// columns; whether it overlaps others is for the client to check.
func validSpans(w http.ResponseWriter, colSpan, rowSpan int) bool {
	switch {
	case colSpan < 0:
		api.WriteError(w, http.StatusBadRequest, "colSpan must be non-negative")
	case colSpan > api.DashboardGridColumns:
		api.WriteError(w, http.StatusBadRequest, fmt.Sprintf("colSpan must be at most %d", api.DashboardGridColumns))
	case rowSpan < 0:
		api.WriteError(w, http.StatusBadRequest, "rowSpan must be non-negative")
	default:
		return true
	}
	return false
}

// DeleteWidget handles DELETE /api/dashboards/{id}/widgets/{widgetId}
func (h *Handlers) DeleteWidget(w http.ResponseWriter, r *http.Request) {
	dashboardID := chi.URLParam(r, "id")
	widgetID := chi.URLParam(r, "widgetId")
	if dashboardID == "" || widgetID == "" {
		api.WriteError(w, http.StatusBadRequest, "dashboard id and widget id are required")
		return
	}

	if err := h.store.DeleteWidget(r.Context(), dashboardID, widgetID); err != nil {
		if errors.Is(err, storage.ErrWidgetNotFound) {
			api.WriteError(w, http.StatusNotFound, "widget not found")
			return
		}
		api.WriteInternalError(w, "failed to delete widget", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

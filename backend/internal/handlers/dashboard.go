package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/storage"
)

// ListDashboards handles GET /api/dashboards
func (h *Handlers) ListDashboards(w http.ResponseWriter, r *http.Request) {
	dashboards, err := h.store.GetDashboards(r.Context())
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, err.Error())
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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.WriteError(w, http.StatusBadRequest, "invalid request body")
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
		api.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	api.WriteJSON(w, http.StatusCreated, dashboard)
}

// GetDefaultDashboard handles GET /api/dashboards/default
func (h *Handlers) GetDefaultDashboard(w http.ResponseWriter, r *http.Request) {
	dashboard, err := h.store.GetDefaultDashboard(r.Context())
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, err.Error())
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
		api.WriteError(w, http.StatusInternalServerError, err.Error())
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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.WriteError(w, http.StatusBadRequest, "invalid request body")
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
		api.WriteError(w, http.StatusInternalServerError, err.Error())
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
		api.WriteError(w, http.StatusInternalServerError, err.Error())
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
		api.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	api.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// CreateWidget handles POST /api/dashboards/{id}/widgets
func (h *Handlers) CreateWidget(w http.ResponseWriter, r *http.Request) {
	dashboardID := chi.URLParam(r, "id")
	if dashboardID == "" {
		api.WriteError(w, http.StatusBadRequest, "dashboard id is required")
		return
	}

	var req api.CreateWidgetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.WriteError(w, http.StatusBadRequest, "invalid request body")
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
	if req.ColSpan < 0 {
		api.WriteError(w, http.StatusBadRequest, "colSpan must be non-negative")
		return
	}
	if req.RowSpan < 0 {
		api.WriteError(w, http.StatusBadRequest, "rowSpan must be non-negative")
		return
	}

	widget, err := h.store.CreateWidget(r.Context(), dashboardID, &req)
	if err != nil {
		if errors.Is(err, storage.ErrDashboardNotFound) {
			api.WriteError(w, http.StatusNotFound, "dashboard not found")
			return
		}
		api.WriteError(w, http.StatusInternalServerError, err.Error())
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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.WriteError(w, http.StatusBadRequest, "invalid request body")
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
		api.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	api.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.WriteError(w, http.StatusBadRequest, "invalid request body")
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
	if req.ColSpan < 0 {
		api.WriteError(w, http.StatusBadRequest, "colSpan must be non-negative")
		return
	}
	if req.RowSpan < 0 {
		api.WriteError(w, http.StatusBadRequest, "rowSpan must be non-negative")
		return
	}

	widget, err := h.store.UpdateWidget(r.Context(), dashboardID, widgetID, &req)
	if err != nil {
		if errors.Is(err, storage.ErrWidgetNotFound) {
			api.WriteError(w, http.StatusNotFound, "widget not found")
			return
		}
		api.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	api.WriteJSON(w, http.StatusOK, widget)
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
		api.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

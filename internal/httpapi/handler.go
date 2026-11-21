package httpapi

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"device-registry/internal/device"
)

type DeviceService interface {
	Register(ctx context.Context, orgID, name, platform string) (device.Device, error)
	List(ctx context.Context, orgID string) ([]device.Device, error)
	Get(ctx context.Context, orgID, id string) (device.Device, error)
	Delete(ctx context.Context, orgID, id string) error
	RecordHeartbeat(ctx context.Context, orgID, id string) error
}

type handler struct {
	svc    DeviceService
	logger *slog.Logger
}

func NewHandler(svc DeviceService, logger *slog.Logger) http.Handler {
	h := &handler{svc: svc, logger: logger}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /orgs/{orgID}/devices", h.register)
	mux.HandleFunc("GET /orgs/{orgID}/devices", h.list)
	mux.HandleFunc("GET /orgs/{orgID}/devices/{deviceID}", h.get)
	mux.HandleFunc("DELETE /orgs/{orgID}/devices/{deviceID}", h.delete)
	mux.HandleFunc("POST /orgs/{orgID}/devices/{deviceID}/heartbeat", h.heartbeat)
	mux.HandleFunc("/", h.routeNotFound)
	return mux
}

type registerRequest struct {
	Name     string `json:"name"`
	Platform string `json:"platform"`
}

type deviceResponse struct {
	ID         string        `json:"id"`
	Name       string        `json:"name"`
	Platform   string        `json:"platform"`
	Status     device.Status `json:"status"`
	LastSeenAt *time.Time    `json:"last_seen_at"`
	CreatedAt  time.Time     `json:"created_at"`
}

func toResponse(d device.Device) deviceResponse {
	resp := deviceResponse{
		ID:        d.ID,
		Name:      d.Name,
		Platform:  d.Platform,
		Status:    d.Status,
		CreatedAt: d.CreatedAt.UTC(),
	}
	if d.LastSeenAt != nil {
		lastSeen := d.LastSeenAt.UTC()
		resp.LastSeenAt = &lastSeen
	}
	return resp
}

func (h *handler) register(w http.ResponseWriter, r *http.Request) {
	orgID := r.PathValue("orgID")

	var req registerRequest
	if err := decodeJSON(w, r, &req); err != nil {
		h.writeError(w, err)
		return
	}

	d, err := h.svc.Register(r.Context(), orgID, req.Name, req.Platform)
	if err != nil {
		h.writeError(w, err)
		return
	}

	w.Header().Set("Location", fmt.Sprintf("/orgs/%s/devices/%s", url.PathEscape(orgID), d.ID))
	h.writeJSON(w, http.StatusCreated, toResponse(d))
}

func (h *handler) list(w http.ResponseWriter, r *http.Request) {
	devices, err := h.svc.List(r.Context(), r.PathValue("orgID"))
	if err != nil {
		h.writeError(w, err)
		return
	}

	resp := make([]deviceResponse, 0, len(devices))
	for _, d := range devices {
		resp = append(resp, toResponse(d))
	}
	h.writeJSON(w, http.StatusOK, resp)
}

func (h *handler) get(w http.ResponseWriter, r *http.Request) {
	d, err := h.svc.Get(r.Context(), r.PathValue("orgID"), r.PathValue("deviceID"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, toResponse(d))
}

func (h *handler) delete(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Delete(r.Context(), r.PathValue("orgID"), r.PathValue("deviceID")); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) heartbeat(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.RecordHeartbeat(r.Context(), r.PathValue("orgID"), r.PathValue("deviceID")); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) routeNotFound(w http.ResponseWriter, _ *http.Request) {
	h.writeAPIError(w, http.StatusNotFound, "route_not_found", "route not found")
}

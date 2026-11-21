package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"device-registry/internal/device"
)

type stubService struct {
	device  device.Device
	devices []device.Device
	err     error

	org, id, name, platform string
}

func (s *stubService) Register(_ context.Context, orgID, name, platform string) (device.Device, error) {
	s.org, s.name, s.platform = orgID, name, platform
	return s.device, s.err
}

func (s *stubService) List(_ context.Context, orgID string) ([]device.Device, error) {
	s.org = orgID
	return s.devices, s.err
}

func (s *stubService) Get(_ context.Context, orgID, id string) (device.Device, error) {
	s.org, s.id = orgID, id
	return s.device, s.err
}

func (s *stubService) Delete(_ context.Context, orgID, id string) error {
	s.org, s.id = orgID, id
	return s.err
}

func (s *stubService) RecordHeartbeat(_ context.Context, orgID, id string) error {
	s.org, s.id = orgID, id
	return s.err
}

func send(svc *stubService, method, path, body string) *httptest.ResponseRecorder {
	h := NewHandler(svc, slog.New(slog.NewTextHandler(io.Discard, nil)))
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("response is not valid JSON: %v\nbody: %s", err, rec.Body.String())
	}
	return v
}

func assertError(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if rec.Code != status {
		t.Errorf("status = %d, want %d (body: %s)", rec.Code, status, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	if got := decode[errorBody](t, rec).Error.Code; got != code {
		t.Errorf("error code = %q, want %q", got, code)
	}
}

var sampleDevice = device.Device{
	ID:        "11111111-1111-1111-1111-111111111111",
	OrgID:     "org-1",
	Name:      "build-01",
	Platform:  "linux",
	Status:    device.StatusOffline,
	CreatedAt: time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC),
}

func TestRegisterCreatesDevice(t *testing.T) {
	svc := &stubService{device: sampleDevice}

	rec := send(svc, http.MethodPost, "/orgs/org-1/devices", `{"name":"build-01","platform":"linux"}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body: %s)", rec.Code, rec.Body.String())
	}
	if svc.org != "org-1" || svc.name != "build-01" || svc.platform != "linux" {
		t.Errorf("service received org=%q name=%q platform=%q", svc.org, svc.name, svc.platform)
	}
	wantLocation := "/orgs/org-1/devices/" + sampleDevice.ID
	if got := rec.Header().Get("Location"); got != wantLocation {
		t.Errorf("Location = %q, want %q", got, wantLocation)
	}

	body := decode[map[string]any](t, rec)
	want := map[string]any{
		"id":           sampleDevice.ID,
		"name":         "build-01",
		"platform":     "linux",
		"status":       "offline",
		"last_seen_at": nil,
		"created_at":   "2026-11-21T12:00:00Z",
	}
	for key, wantValue := range want {
		if gotValue, ok := body[key]; !ok || gotValue != wantValue {
			t.Errorf("%s = %v (present: %t), want %v", key, gotValue, ok, wantValue)
		}
	}
}

func TestRegisterRejectsMalformedBodies(t *testing.T) {
	tests := map[string]string{
		"empty body":      ``,
		"not json":        `not json`,
		"array":           `[]`,
		"unknown field":   `{"name":"a","platform":"linux","color":"red"}`,
		"wrong type":      `{"name":1,"platform":"linux"}`,
		"trailing object": `{"name":"a","platform":"linux"}{"name":"b"}`,
	}

	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			svc := &stubService{}

			rec := send(svc, http.MethodPost, "/orgs/org-1/devices", body)

			assertError(t, rec, http.StatusBadRequest, "invalid_input")
			if svc.name != "" {
				t.Error("service must not be called for a malformed body")
			}
		})
	}
}

func TestErrorsAreMappedToStatusAndCode(t *testing.T) {
	tests := map[string]struct {
		err    error
		status int
		code   string
	}{
		"invalid input": {&device.InvalidInputError{Message: "name is required"}, http.StatusBadRequest, "invalid_input"},
		"not found":     {device.ErrNotFound, http.StatusNotFound, "device_not_found"},
		"name taken":    {device.ErrNameTaken, http.StatusConflict, "device_name_taken"},
		"limit reached": {device.ErrLimitReached, http.StatusConflict, "device_limit_reached"},
		"wrapped":       {errors.Join(errors.New("context"), device.ErrNameTaken), http.StatusConflict, "device_name_taken"},
		"unexpected":    {errors.New("connection refused"), http.StatusInternalServerError, "internal_error"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			rec := send(&stubService{err: tc.err}, http.MethodPost, "/orgs/org-1/devices", `{"name":"a","platform":"linux"}`)

			assertError(t, rec, tc.status, tc.code)
		})
	}
}

func TestInternalErrorDoesNotLeakDetails(t *testing.T) {
	rec := send(&stubService{err: errors.New("password=secret host=db")}, http.MethodGet, "/orgs/org-1/devices/x", "")

	if strings.Contains(rec.Body.String(), "secret") {
		t.Errorf("response leaks internal error: %s", rec.Body.String())
	}
}

func TestListReturnsEmptyArrayWhenNoDevices(t *testing.T) {
	rec := send(&stubService{}, http.MethodGet, "/orgs/org-1/devices", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != "[]" {
		t.Errorf("body = %s, want []", got)
	}
}

func TestListReturnsDevicesWithLastSeenInUTC(t *testing.T) {
	zone := time.FixedZone("UTC+3", 3*60*60)
	seen := time.Date(2026, 11, 21, 15, 0, 0, 0, zone)
	d := sampleDevice
	d.Status, d.LastSeenAt = device.StatusOnline, &seen

	rec := send(&stubService{devices: []device.Device{d}}, http.MethodGet, "/orgs/org-1/devices", "")

	got := decode[[]deviceResponse](t, rec)
	if len(got) != 1 || got[0].Status != device.StatusOnline {
		t.Fatalf("got %+v", got)
	}
	if want := "2026-11-21T12:00:00Z"; !strings.Contains(rec.Body.String(), want) {
		t.Errorf("body %s does not contain UTC time %s", rec.Body.String(), want)
	}
}

func TestGetPassesPathValuesToService(t *testing.T) {
	svc := &stubService{device: sampleDevice}

	rec := send(svc, http.MethodGet, "/orgs/org-1/devices/abc", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if svc.org != "org-1" || svc.id != "abc" {
		t.Errorf("service received org=%q id=%q", svc.org, svc.id)
	}
}

func TestGetUnknownDeviceIsNotFound(t *testing.T) {
	rec := send(&stubService{err: device.ErrNotFound}, http.MethodGet, "/orgs/org-1/devices/abc", "")

	assertError(t, rec, http.StatusNotFound, "device_not_found")
}

func TestDeleteAndHeartbeatReturnNoContent(t *testing.T) {
	tests := map[string]struct{ method, path string }{
		"delete":    {http.MethodDelete, "/orgs/org-1/devices/abc"},
		"heartbeat": {http.MethodPost, "/orgs/org-1/devices/abc/heartbeat"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			svc := &stubService{}

			rec := send(svc, tc.method, tc.path, "")

			if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
				t.Errorf("status = %d, body = %q; want 204 and empty body", rec.Code, rec.Body.String())
			}
			if svc.org != "org-1" || svc.id != "abc" {
				t.Errorf("service received org=%q id=%q", svc.org, svc.id)
			}
		})
	}
}

func TestDeleteAndHeartbeatOfUnknownDeviceAreNotFound(t *testing.T) {
	tests := map[string]struct{ method, path string }{
		"delete":    {http.MethodDelete, "/orgs/org-1/devices/abc"},
		"heartbeat": {http.MethodPost, "/orgs/org-1/devices/abc/heartbeat"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			rec := send(&stubService{err: device.ErrNotFound}, tc.method, tc.path, "")

			assertError(t, rec, http.StatusNotFound, "device_not_found")
		})
	}
}

func TestUnknownRouteReturnsJSONNotFound(t *testing.T) {
	rec := send(&stubService{}, http.MethodGet, "/nope", "")

	assertError(t, rec, http.StatusNotFound, "route_not_found")
}

package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"device-registry/internal/device"
)

const maxBodyBytes = 1 << 20

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		return &device.InvalidInputError{Message: "request body must be a valid JSON object: " + err.Error()}
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return &device.InvalidInputError{Message: "request body must contain a single JSON object"}
	}
	return nil
}

func (h *handler) writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		h.logger.Error("write response", "error", err)
	}
}

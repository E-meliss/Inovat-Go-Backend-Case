package httpapi

import (
	"errors"
	"net/http"

	"device-registry/internal/device"
)

type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (h *handler) writeError(w http.ResponseWriter, err error) {
	var invalid *device.InvalidInputError

	switch {
	case errors.As(err, &invalid):
		h.writeAPIError(w, http.StatusBadRequest, "invalid_input", invalid.Message)
	case errors.Is(err, device.ErrNotFound):
		h.writeAPIError(w, http.StatusNotFound, "device_not_found", device.ErrNotFound.Error())
	case errors.Is(err, device.ErrNameTaken):
		h.writeAPIError(w, http.StatusConflict, "device_name_taken", device.ErrNameTaken.Error())
	case errors.Is(err, device.ErrLimitReached):
		h.writeAPIError(w, http.StatusConflict, "device_limit_reached", device.ErrLimitReached.Error())
	default:
		h.logger.Error("unhandled error", "error", err)
		h.writeAPIError(w, http.StatusInternalServerError, "internal_error", "internal server error")
	}
}

func (h *handler) writeAPIError(w http.ResponseWriter, status int, code, message string) {
	h.writeJSON(w, status, errorBody{Error: errorDetail{Code: code, Message: message}})
}

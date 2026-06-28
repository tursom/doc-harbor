package app

import (
	"encoding/json"
	"errors"
	"net/http"
)

type appError struct {
	Status  int
	Message string
}

type structuredAppError struct {
	Status  int
	Code    string
	Message string
	Detail  string
}

func (e appError) Error() string {
	return e.Message
}

func (e structuredAppError) Error() string {
	if e.Detail != "" {
		return e.Message + ": " + e.Detail
	}
	return e.Message
}

func errBadRequest(message string) error {
	return appError{Status: http.StatusBadRequest, Message: message}
}

func errUnauthorized(message string) error {
	return appError{Status: http.StatusUnauthorized, Message: message}
}

func errNotFound(message string) error {
	return appError{Status: http.StatusNotFound, Message: message}
}

func errConflict(message string) error {
	return appError{Status: http.StatusConflict, Message: message}
}

func errUnavailable(message string) error {
	return appError{Status: http.StatusServiceUnavailable, Message: message}
}

func errInternal(message string) error {
	return appError{Status: http.StatusInternalServerError, Message: message}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, err error) {
	var appErr appError
	if errors.As(err, &appErr) {
		writeJSON(w, appErr.Status, map[string]any{"error": appErr.Message})
		return
	}
	writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
}

func writeStructuredError(w http.ResponseWriter, status int, code, message, detail string) {
	body := map[string]any{
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
	}
	if detail != "" {
		body["error"].(map[string]any)["detail"] = detail
	}
	writeJSON(w, status, body)
}

func writeFieldErrors(w http.ResponseWriter, fields map[string]string) {
	writeJSON(w, http.StatusBadRequest, map[string]any{
		"error": map[string]any{
			"code":    "validation_failed",
			"message": "配置不完整",
		},
		"fields": fields,
	})
}

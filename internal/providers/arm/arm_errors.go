package arm

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"
)

type armErrorBody struct {
	Error armErrorInner `json:"error"`
}

type armErrorInner struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Target  string `json:"target,omitempty"`
}

func writeARMError(w http.ResponseWriter, status int, code, message string) {
	setARMHeaders(w)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(armErrorBody{
		Error: armErrorInner{Code: code, Message: message},
	})
}

func writeARMJSON(w http.ResponseWriter, status int, v any) {
	setARMHeaders(w)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func setARMHeaders(w http.ResponseWriter) {
	w.Header().Set("x-ms-request-id", newID())
	w.Header().Set("x-ms-correlation-request-id", newID())
	w.Header().Set("Date", time.Now().UTC().Format(http.TimeFormat))
}

func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

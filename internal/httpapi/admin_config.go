package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/jiayu/wbot/internal/config"
)

const adminConfigListPath = "/v1/admin/config"

// ConfigStore is the configuration surface the admin config endpoints need.
type ConfigStore interface {
	List() ([]config.Entry, error)
	Set(key, value string) error
}

// ConfigHandler returns an http.Handler serving GET /v1/admin/config and PUT /v1/admin/config/{key}.
// Values are never returned: GET lists key metadata only, PUT responds with set:true (PRIVACY).
func ConfigHandler(store ConfigStore) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc(adminConfigListPath, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		entries, err := store.List()
		if err != nil {
			fmt.Fprintf(os.Stderr, "httpapi: admin: config: list: %v\n", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(entries)
	})

	mux.HandleFunc(adminConfigListPath+"/{key}", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		var req struct {
			Value string `json:"value"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		key := r.PathValue("key")
		if err := store.Set(key, req.Value); err != nil {
			switch {
			case errors.Is(err, config.ErrUnknownKey):
				writeError(w, http.StatusNotFound, "unknown config key")
			case errors.Is(err, config.ErrEmptyValue), errors.Is(err, config.ErrValueTooLong):
				writeError(w, http.StatusBadRequest, err.Error())
			default:
				fmt.Fprintf(os.Stderr, "httpapi: admin: config: set %s: %v\n", key, err)
				writeError(w, http.StatusInternalServerError, "internal error")
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"key": key, "set": true})
	})

	// Any other path under /v1/admin/config: JSON 404.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not found")
	})

	return mux
}

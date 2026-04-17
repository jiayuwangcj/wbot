package httpregister

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/jiayu/wbot/internal/master"
)

const registerPath = "/v1/register"

// RegisterRequest is the JSON body for POST /v1/register.
type RegisterRequest struct {
	ID string `json:"id"`
}

// RegisterResponse reports whether the ID was newly recorded.
type RegisterResponse struct {
	New bool `json:"new"`
}

// Handler returns an http.Handler that serves POST /v1/register and forwards
// to the given facade.
func Handler(f master.Facade) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != registerPath {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		ct := r.Header.Get("Content-Type")
		if ct != "" && !strings.HasPrefix(ct, "application/json") {
			http.Error(w, "unsupported media type", http.StatusUnsupportedMediaType)
			return
		}
		defer r.Body.Close()
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		var req RegisterRequest
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		newID := f.Register(req.ID)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(RegisterResponse{New: newID})
	})
}

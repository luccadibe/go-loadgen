package main

import (
	"encoding/json"
	"io"
	"net/http"
	"sync/atomic"
)

var (
	counter atomic.Int32
)

func main() {
	http.HandleFunc("/increment", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Only POST allowed", http.StatusMethodNotAllowed)
			return
		}
		var delta int32
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Failed to read body", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		var req struct {
			Delta *int32 `json:"delta"`
		}
		if err := json.Unmarshal(body, &req); err == nil && req.Delta != nil {
			delta = *req.Delta
		} else {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		counter.Add(delta)
		current := counter.Load()

		resp := map[string]int32{"counter": current}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	http.ListenAndServe(":8080", nil)
}

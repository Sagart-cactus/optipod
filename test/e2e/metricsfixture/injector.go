package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"
)

// This separate test-only TLS mode is an admission injector used to exercise
// webhook ordering and reinvocation against a real API server.
func runInjector() {
	mux := http.NewServeMux()
	mux.HandleFunc("/inject", func(w http.ResponseWriter, r *http.Request) {
		var review struct {
			Request struct {
				UID    string `json:"uid"`
				Object struct {
					Spec struct {
						Containers []struct {
							Name string `json:"name"`
						} `json:"containers"`
					} `json:"spec"`
				} `json:"object"`
			} `json:"request"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&review); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		response := map[string]any{"uid": review.Request.UID, "allowed": true}
		found := false
		for _, c := range review.Request.Object.Spec.Containers {
			if c.Name == "injected-proxy" {
				found = true
			}
		}
		if !found {
			patch, _ := json.Marshal([]any{map[string]any{"op": "add", "path": "/spec/containers/-", "value": map[string]any{"name": "injected-proxy", "image": os.Getenv("INJECTED_IMAGE"), "resources": map[string]any{"requests": map[string]string{"cpu": "50m", "memory": "32Mi"}}}}})
			response["patchType"] = "JSONPatch"
			response["patch"] = patch
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"apiVersion": "admission.k8s.io/v1", "kind": "AdmissionReview", "response": response})
	})
	srv := &http.Server{Addr: ":9443", Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	log.Fatal(srv.ListenAndServeTLS(os.Getenv("INJECTOR_TLS_CERT"), os.Getenv("INJECTOR_TLS_KEY")))
}

// A controllable load generator used only in the isolated E2E cluster.
package main

import (
	"fmt"
	"math"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"sync/atomic"
	"time"
)

var cpu atomic.Int64
var memory atomic.Int64
var observed atomic.Int64
var sink float64

func envInt(name string, def int64) int64 {
	v, err := strconv.ParseInt(os.Getenv(name), 10, 64)
	if err != nil {
		return def
	}
	return v
}

func main() {
	if seconds := envInt("EXIT_AFTER_SECONDS", 0); seconds > 0 {
		time.AfterFunc(time.Duration(seconds)*time.Second, func() { os.Exit(0) })
	}
	cpu.Store(envInt("CPU_MILLICORES", 200))
	memory.Store(envInt("MEMORY_MIB", 40))
	go func() {
		for {
			start := time.Now()
			budget := time.Duration(cpu.Load()) * time.Millisecond / 10
			for time.Since(start) < budget {
				sink = math.Sqrt(sink + 42)
			}
			if remaining := 100*time.Millisecond - time.Since(start); remaining > 0 {
				time.Sleep(remaining)
			}
		}
	}()
	go func() {
		var data []byte
		for {
			bytes := int(memory.Load()) * 1024 * 1024
			if len(data) != bytes {
				data = make([]byte, bytes)
			}
			for i := 0; i < len(data); i += 4096 {
				data[i]++
			}
			observed.Store(int64(len(data)))
			runtime.KeepAlive(data)
			time.Sleep(time.Second)
		}
	}()
	http.HandleFunc("/ready", func(w http.ResponseWriter, _ *http.Request) { _, _ = fmt.Fprintln(w, "ready") })
	http.HandleFunc("/metrics", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		_, _ = fmt.Fprintf(w, "# TYPE jvm_memory_used_bytes gauge\njvm_memory_used_bytes{area=\"heap\"} %d\n# TYPE app_queue_depth gauge\napp_queue_depth 20\n", observed.Load())
	})
	http.HandleFunc("/configure", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		for name, target := range map[string]*atomic.Int64{"cpu": &cpu, "memory_mib": &memory} {
			if raw := r.URL.Query().Get(name); raw != "" {
				v, err := strconv.ParseInt(raw, 10, 64)
				if err != nil || v < 0 || v > 1024 {
					http.Error(w, "invalid load", 400)
					return
				}
				target.Store(v)
			}
		}
		_, _ = fmt.Fprintln(w, "configured")
	})
	if err := http.ListenAndServe(":8080", nil); err != nil {
		panic(err)
	}
}

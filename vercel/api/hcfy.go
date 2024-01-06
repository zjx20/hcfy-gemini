package api

import (
	"net/http"
	"os"

	"github.com/zjx20/hcfy-gemini/hcfy"
)

var (
	mux *http.ServeMux
)

func init() {
	mux = http.NewServeMux()
	mux.Handle("/api/hcfy", http.HandlerFunc(hcfy.Handle))
}

func Handler(w http.ResponseWriter, r *http.Request) {
	if token := os.Getenv("PASSWORD"); token != "" {
		if r.URL.Query().Get("pass") != token {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte("bad password"))
			return
		}
	}
	mux.ServeHTTP(w, r)
}

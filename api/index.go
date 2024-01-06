package api

import (
	"net/http"

	"github.com/zjx20/hcfy-gemini/hcfy"
)

var (
	mux *http.ServeMux
)

func init() {
	mux = http.NewServeMux()
	mux.Handle("/hcfyCustom", http.HandlerFunc(hcfy.Handle))
}

func Handler(w http.ResponseWriter, r *http.Request) {
	mux.ServeHTTP(w, r)
}

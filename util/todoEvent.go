package util

import (
	"net/http"

	m "github.com/go-chi/chi/v5/middleware"
	log "github.com/sirupsen/logrus"

	"github.com/zjx20/hcfy-gemini/config"
)

func TodoEvent(w http.ResponseWriter) {
	_, err := w.Write([]byte{})
	if err != nil {
		log.Errorln(err)
		if config.GetIsDebug() {
			m.PrintPrettyStack(err)
		}
	}
}

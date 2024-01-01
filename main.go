package main

import (
	"net"
	"net/http"
	"os"
	"runtime"

	"github.com/zjx20/hcfy-gemini/config"
	"github.com/zjx20/hcfy-gemini/translate"
	"github.com/zjx20/hcfy-gemini/util/middleware"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
	log "github.com/sirupsen/logrus"
)

func init() {
	log.SetLevel(config.GetLogLevel())
	log.SetOutput(os.Stdout)
	log.SetFormatter(&log.TextFormatter{
		DisableColors:   runtime.GOOS == "windows",
		FullTimestamp:   true,
		TimestampFormat: "2006-01-02 15:04:05",
	})
	config.AddConfigChangeCallback(func() {
		log.SetLevel(config.GetLogLevel())
	})
}

func main() {
	config.Init()
	r := chi.NewRouter()

	r.Use(middleware.Logger)
	r.Use(middleware.Recover)

	r.Post("/hcfyCustom", hcfyCustom)

	l, err := net.Listen("tcp", ":7458")
	if err != nil {
		log.Fatalln(err)
	}
	log.Infof("Server listening at %s", l.Addr())
	if err = http.Serve(l, r); err != nil {
		log.Fatalln(err)
	}
}

func hcfyCustom(w http.ResponseWriter, r *http.Request) {
	req := &translate.TranslateReq{}
	if err := render.Bind(r, req); err != nil {
		log.Debugf("bad request: %s", err)
		render.Status(r, http.StatusBadRequest)
		render.PlainText(w, r, err.Error())
		return
	}
	ch := make(chan *translate.TranslateResult, 1)
	translate.Translate(req, ch)
	select {
	case <-r.Context().Done():
		log.Errorf("context break, reason: %s, req: %+v", r.Context().Err(), req)
		render.Status(r, http.StatusInternalServerError)
		render.PlainText(w, r, r.Context().Err().Error())
		return
	case result := <-ch:
		if result.Err != nil {
			render.Status(r, http.StatusInternalServerError)
			render.PlainText(w, r, result.Err.Error())
			return
		} else {
			render.JSON(w, r, result.Resp)
			return
		}
	}
}

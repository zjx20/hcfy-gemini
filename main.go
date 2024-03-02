package main

import (
	"net"
	"net/http"
	"os"
	"runtime"

	"github.com/zjx20/hcfy-gemini/cjsfy"
	"github.com/zjx20/hcfy-gemini/config"
	"github.com/zjx20/hcfy-gemini/hcfy"
	"github.com/zjx20/hcfy-gemini/util/middleware"

	"github.com/go-chi/chi/v5"
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
	if noConf := os.Getenv("NO_CONFIG_FILE"); noConf == "" {
		config.Init()
	}
	r := chi.NewRouter()

	r.Use(middleware.Logger)
	r.Use(middleware.Recover)

	r.Post("/api/hcfy", hcfy.Handle)
	r.Post("/api/cjsfy", cjsfy.Handle)

	l, err := net.Listen("tcp", ":7458")
	if err != nil {
		log.Fatalln(err)
	}
	log.Infof("Server listening at %s", l.Addr())
	if err = http.Serve(l, r); err != nil {
		log.Fatalln(err)
	}
}

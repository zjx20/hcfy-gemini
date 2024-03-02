package translate

import (
	"fmt"
	"strings"

	log "github.com/sirupsen/logrus"
)

const (
	maxConcurrent = 100
)

var sem = make(chan string, maxConcurrent)

func init() {
	for i := 0; i < cap(sem); i++ {
		sem <- fmt.Sprintf("translate_%d", i)
	}
}

func goFire(s *session) {
	id := <-sem
	defer func() {
		sem <- id
	}()
	s.fire(id)
}

func Translate(req *TranslateReq, ch chan *TranslateResult) {
	req.Text = strings.TrimSpace(req.Text)
	if len(req.Destination) == 0 || req.Text == "" {
		log.Errorf("bad translate req: %+v", req)
		return
	}
	s := newSession(req.Destination, strings.Split(req.Text, "\n"), ch)
	go goFire(s)
}

func Translate2(input []string, to string, ch chan *TranslateResult) {
	s := newSession([]string{to}, input, ch)
	go goFire(s)
}

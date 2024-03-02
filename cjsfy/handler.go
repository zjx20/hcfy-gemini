package cjsfy

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/render"
	log "github.com/sirupsen/logrus"
	"github.com/zjx20/hcfy-gemini/translate"
	"github.com/zjx20/hcfy-gemini/util/tokenbucket"
)

const splitter = "-----splitter-----"

var tokenBucket = tokenbucket.NewAdaptiveTokenBucket(60, 60,
	tokenbucket.ProductionRule{
		Interval:  1 * time.Minute,
		Increment: 60,
	},
	[]tokenbucket.ConsumptionRule{
		{
			RestThreshold: 40,
			Wait:          0,
			RuleID:        1,
		},
		{
			RestThreshold: 30,
			Wait:          100 * time.Millisecond,
			RuleID:        2,
		},
		{
			RestThreshold: 20,
			Wait:          500 * time.Millisecond,
			RuleID:        3,
		},
		{
			RestThreshold: 10,
			Wait:          2000 * time.Millisecond,
			RuleID:        4,
		},
		{
			RestThreshold: 0,
			Wait:          3000 * time.Millisecond,
			RuleID:        5,
		},
	},
)

var mergeRules = []struct {
	roleID   int
	maxBytes int
}{
	{1, 600},
	{2, 1200},
	{3, 1500},
	{4, 1800},
	{5, 2000},
}

func mergeMaxBytes(ruleID int) int {
	for _, x := range mergeRules {
		if x.roleID == ruleID {
			return x.maxBytes
		}
	}
	return mergeRules[len(mergeRules)-1].maxBytes
}

var inputCh = make(chan *request) // no buffer

type request struct {
	text     string
	to       string
	cancelCh chan struct{}
	respCh   chan *response
}

type response struct {
	translatedText string
}

func translateRuntine(input <-chan *request) {
	var nextHeadReq *request
	for {
		req := nextHeadReq
		nextHeadReq = nil
		if req == nil {
			req = <-input
		}
		ruleID, err := tokenBucket.Consume(context.Background())
		if err != nil {
			log.Errorf("translateRuntine exit, err: %v", err)
			return
		}
		maxBytes := mergeMaxBytes(ruleID)
		var requests []*request
		time.Sleep(300 * time.Millisecond) // wait for more incoming requests
		requests, nextHeadReq = collect(req, maxBytes, input)
		go handleRequests(requests, false)
	}
}

func collect(headReq *request, maxBytes int, input <-chan *request) ([]*request, *request) {
	requests := []*request{headReq}
	sum := len(headReq.text)
	to := headReq.to
	done := false
	for !done && sum < maxBytes {
		select {
		case req := <-input:
			if req.to != to {
				return requests, req
			}
			sum += len(req.text)
			requests = append(requests, req)
		default:
			done = true
		}
	}
	return requests, nil
}

func allCanceledCh(requests []*request) <-chan struct{} {
	wg := &sync.WaitGroup{}
	for _, r := range requests {
		r := r
		wg.Add(1)
		go func() {
			<-r.cancelCh
			wg.Done()
		}()
	}
	resultCh := make(chan struct{})
	go func() {
		wg.Wait()
		close(resultCh)
	}()
	return resultCh
}

func handleRequests(requests []*request, needToken bool) {
	log.Debugf("handleRequests len: %d", len(requests))
	doneCh := allCanceledCh(requests)
	for {
		if needToken {
			_, err := tokenBucket.Consume(context.Background())
			if err != nil {
				return
			}
		}
		needToken = true

		type holder struct {
			req    *request
			result []string
		}
		var holders []*holder

		var mapping []int
		var input []string
		for idx, r := range requests {
			holders = append(holders, &holder{req: r})
			for _, x := range strings.Split(r.text, "<Keep This Symbol>") {
				input = append(input, strings.TrimSpace(x))
				mapping = append(mapping, idx)
			}
		}
		ch := make(chan *translate.TranslateResult, 1)
		translate.Translate2(input, requests[0].to, ch)
		select {
		case <-doneCh:
			return
		case result := <-ch:
			if result.Err != nil {
				log.Errorf("translate error: %s", result.Err)
				// retry
				break
			}
			if len(result.Resp.Result) != len(input) {
				log.Errorf("number of translation result (%d) doesn't match the request (%d)",
					len(result.Resp.Result), len(input))
				// retry
				break
			}
			for idx, result := range result.Resp.Result {
				holderIdx := mapping[idx]
				holder := holders[holderIdx]
				holder.result = append(holder.result, result)
			}
			for _, holder := range holders {
				holder.req.respCh <- &response{
					translatedText: strings.Join(holder.result, "\n<Keep This Symbol>\n"),
				}
			}
			return
		}
	}
}

func init() {
	go translateRuntine(inputCh)
}

func Handle(w http.ResponseWriter, r *http.Request) {
	if token := os.Getenv("PASSWORD"); token != "" {
		if r.URL.Query().Get("pass") != token {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte("bad password"))
			return
		}
	}
	req := &GeminiAPIRequest{}
	if err := render.Decode(r, req); err != nil {
		log.Debugf("bad request: %s", err)
		render.Status(r, http.StatusBadRequest)
		render.PlainText(w, r, err.Error())
		return
	}

	if len(req.Contents) != 1 || len(req.Contents[0].Parts) != 1 {
		log.Errorf("bad request: %+v", req)
		render.Status(r, http.StatusBadRequest)
		render.PlainText(w, r, "expect Contents and Contents[0].Parts has only one element")
		return
	}
	input := req.Contents[0].Parts[0].Text
	parts := strings.Split(input, splitter)
	if len(parts) != 2 {
		log.Errorf("bad text input: %+v", input)
		render.Status(r, http.StatusBadRequest)
		render.PlainText(w, r, "expect Contents[0].Parts[0].Text has two parts")
		return
	}
	to := strings.TrimSpace(parts[0])
	text := strings.TrimSpace(parts[1])
	respCh := make(chan *response, 1)
	cancelCh := make(chan struct{})
	defer close(cancelCh)

	log.Debugf("cjsfy request, to: %s text: %s", to, text)

	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()

	transReq := &request{
		text:     text,
		to:       to,
		cancelCh: cancelCh,
		respCh:   respCh,
	}
	select {
	case inputCh <- transReq:
	case <-ctx.Done():
		render.Status(r, http.StatusInternalServerError)
		render.PlainText(w, r, fmt.Sprintf("Internal Server Error: %s", ctx.Err()))
		return
	}

	select {
	case result := <-respCh:
		log.Debugf("cjsfy get response, translated text: %s", result.translatedText)
		resp := &GeminiAPIResponse{
			Candidates: []*Candidate{
				{
					Content: &Content{
						Parts: []*Part{
							{
								Text: result.translatedText,
							},
						},
					},
				},
			},
		}
		render.JSON(w, r, resp)
		return
	case <-ctx.Done():
		render.Status(r, http.StatusInternalServerError)
		render.PlainText(w, r, fmt.Sprintf("Internal Server Error: %s", ctx.Err()))
		return
	}
}

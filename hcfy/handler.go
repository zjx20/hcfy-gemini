package hcfy

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/render"
	log "github.com/sirupsen/logrus"
	"github.com/zjx20/hcfy-gemini/translate"
	"github.com/zjx20/hcfy-gemini/util/tokenbucket"
)

var tokenBucket = tokenbucket.NewAdaptiveTokenBucket(60, 60,
	tokenbucket.ProductionRule{
		Interval:  1 * time.Minute,
		Increment: 60,
	},
	[]tokenbucket.ConsumptionRule{
		{
			Threshold: 40,
			Wait:      0,
			RuleID:    1,
		},
		{
			Threshold: 30,
			Wait:      100 * time.Millisecond,
			RuleID:    2,
		},
		{
			Threshold: 20,
			Wait:      500 * time.Millisecond,
			RuleID:    3,
		},
		{
			Threshold: 10,
			Wait:      2000 * time.Millisecond,
			RuleID:    4,
		},
		{
			Threshold: 0,
			Wait:      3000 * time.Millisecond,
			RuleID:    5,
		},
	},
)

var splitRules = []struct {
	ruleID   int
	maxParts int
}{
	{1, 8},
	{2, 4},
	{3, 3},
	{4, 2},
	{5, 1},
}

type subReq struct {
	lines     []string
	index     []int
	totalChar int
}

func split(req *translate.TranslateReq, ruleID int) []*subReq {
	parts := 1
	for _, rule := range splitRules {
		if rule.ruleID == ruleID {
			parts = rule.maxParts
			break
		}
	}
	if parts == 1 {
		lines := strings.Split(req.Text, "\n")
		index := make([]int, len(lines))
		for i := range lines {
			index[i] = i
		}
		return []*subReq{{
			lines: lines,
			index: index,
		}}
	}
	type tmpLine struct {
		line  string
		index int
	}
	var lines []*tmpLine
	for i, line := range strings.Split(req.Text, "\n") {
		lines = append(lines, &tmpLine{line, i})
	}
	// sort by length of the line, in reverse order
	slices.SortFunc(lines, func(a, b *tmpLine) int {
		return len(b.line) - len(a.line)
	})

	// split the request as even as possible
	res := make([]*subReq, parts)
	for i := 0; i < parts; i++ {
		res[i] = &subReq{}
	}
	for _, l := range lines {
		minTotal := 0
		picked := -1
		for i := range res {
			if picked == -1 || minTotal > res[i].totalChar {
				minTotal = res[i].totalChar
				picked = i
			}
		}
		subReq := res[picked]
		subReq.lines = append(subReq.lines, l.line)
		subReq.index = append(subReq.index, l.index)
		subReq.totalChar += len(l.line)
	}
	for i := len(res) - 1; i >= 0; i-- {
		if res[i].totalChar == 0 {
			res = res[:i]
		}
	}
	return res
}

func handleSubReq(ctx context.Context, req *translate.TranslateReq, sub *subReq, needToken bool) *translate.TranslateResult {
	for {
		if needToken {
			_, err := tokenBucket.Consume(ctx)
			if err != nil {
				return &translate.TranslateResult{
					Err: err,
				}
			}
		}
		needToken = true
		ch := make(chan *translate.TranslateResult, 1)
		cloneReq := *req
		cloneReq.Text = strings.Join(sub.lines, "\n")
		translate.Translate(&cloneReq, ch)
		select {
		case <-ctx.Done():
			return &translate.TranslateResult{
				Err: ctx.Err(),
			}
		case result := <-ch:
			if result.Err != nil {
				log.Errorf("translate error: %s", result.Err)
				// retry
			} else {
				return result
			}
		}
	}
}

func reconstructResult(req *translate.TranslateReq, subReqs []*subReq, results []*translate.TranslateResult) *translate.TranslateResult {
	for idx, result := range results {
		if result.Err != nil {
			log.Errorf("sub request %d failed, err: %s", idx, result.Err)
			return result
		}
		if len(subReqs[idx].lines) != len(result.Resp.Result) {
			log.Errorf("sub request %d has %d lines, but result has %d lines",
				idx, len(subReqs[idx].lines), len(result.Resp.Result))
			return &translate.TranslateResult{
				Err: fmt.Errorf("invalid result, sub request %d has %d lines, but result has %d lines",
					idx, len(subReqs[idx].lines), len(result.Resp.Result)),
			}
		}
	}
	totalLines := 0
	for _, sub := range subReqs {
		totalLines += len(sub.lines)
	}
	lines := make([]string, totalLines)
	for idx := range subReqs {
		subReq := subReqs[idx]
		result := results[idx]
		for lIdx := range subReq.index {
			lines[subReq.index[lIdx]] = result.Resp.Result[lIdx]
		}
	}
	resp := *results[0].Resp
	resp.Text = req.Text
	resp.Result = lines
	return &translate.TranslateResult{
		Resp: &resp,
	}
}

func Handle(w http.ResponseWriter, r *http.Request) {
	req := &translate.TranslateReq{}
	if err := render.Bind(r, req); err != nil {
		log.Debugf("bad request: %s", err)
		render.Status(r, http.StatusBadRequest)
		render.PlainText(w, r, err.Error())
		return
	}
	req.Text = strings.TrimSpace(req.Text)
	if req.Text == "" {
		render.Status(r, http.StatusBadRequest)
		render.PlainText(w, r, "empty text")
		return
	}
	ruleID, err := tokenBucket.Consume(r.Context())
	if err != nil {
		log.Errorf("token bucket consume error: %s", err)
		render.Status(r, http.StatusInternalServerError)
		render.PlainText(w, r, err.Error())
		return
	}
	subReqs := split(req, ruleID)
	log.Debugf("request has been splitted into %d sub requests", len(subReqs))
	results := make([]*translate.TranslateResult, len(subReqs))
	ch := make(chan struct{}, len(subReqs))
	for idx, subReq := range subReqs {
		idx := idx
		subReq := subReq
		go func() {
			result := handleSubReq(r.Context(), req, subReq, idx != 0)
			results[idx] = result
			ch <- struct{}{}
		}()
	}
	cnt := 0
	for cnt < len(subReqs) {
		select {
		case <-ch:
			cnt++
		case <-r.Context().Done():
			log.Errorf("context done before all results are collected, cnt: %d, err: %s", cnt, err)
			render.Status(r, http.StatusInternalServerError)
			render.PlainText(w, r, err.Error())
			return
		}
	}

	result := reconstructResult(req, subReqs, results)
	if result.Err != nil {
		render.Status(r, http.StatusInternalServerError)
		render.PlainText(w, r, result.Err.Error())
		return
	} else {
		render.JSON(w, r, result.Resp)
		return
	}
}

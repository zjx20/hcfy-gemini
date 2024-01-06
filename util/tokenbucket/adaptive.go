package tokenbucket

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type ProductionRule struct {
	Interval  time.Duration
	Increment int
}

func (p *ProductionRule) validate() error {
	if p.Interval == 0 {
		return fmt.Errorf("interval must be greater than 0")
	}
	if p.Increment == 0 {
		return fmt.Errorf("increment must be greater than 0")
	}
	return nil
}

type ConsumptionRule struct {
	Threshold int
	Wait      time.Duration
	RuleID    int
}

type AdaptiveTokenBucket struct {
	mu            sync.Mutex
	maxTokens     int
	currTokens    int
	lastTs        time.Time
	nextProduceTs time.Time
	prodRule      ProductionRule
	consRules     []ConsumptionRule
	produceCh     chan struct{}
	stopCh        chan struct{}
	stopOnce      sync.Once
}

func NewAdaptiveTokenBucket(maxTokens int, initialTokens int, prodRule ProductionRule, consRules []ConsumptionRule) *AdaptiveTokenBucket {
	if err := prodRule.validate(); err != nil {
		panic(fmt.Sprintf("invalid production rule: %s", err))
	}
	if maxTokens <= 0 {
		panic("maxTokens must be greater than 0")
	}
	bucket := &AdaptiveTokenBucket{
		maxTokens:     maxTokens,
		currTokens:    initialTokens,
		nextProduceTs: time.Now().Add(prodRule.Interval),
		prodRule:      prodRule,
		consRules:     consRules,
		produceCh:     make(chan struct{}, 1),
		stopCh:        make(chan struct{}),
	}
	go bucket.produceLoop()
	return bucket
}

func (b *AdaptiveTokenBucket) produce() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.currTokens += b.prodRule.Increment
	if b.currTokens > b.maxTokens {
		b.currTokens = b.maxTokens
	}
	b.nextProduceTs = time.Now().Add(b.prodRule.Interval)
}

func (b *AdaptiveTokenBucket) produceLoop() {
	timer := time.NewTicker(b.prodRule.Interval)
	defer timer.Stop()
	for {
		select {
		case <-b.stopCh:
			return
		case <-timer.C:
			b.produce()
			select {
			case b.produceCh <- struct{}{}:
			default:
			}
		}
	}
}

func (b *AdaptiveTokenBucket) Consume(ctx context.Context) (ruleID int, err error) {
	for {
		consumed, noToken, wait, ruleID := b.tryConsume()
		if consumed {
			return ruleID, nil
		}
		if noToken {
			select {
			case <-b.produceCh:
				// wake the next consumer to simulate broadcast notification
				select {
				case b.produceCh <- struct{}{}:
				default:
				}
				continue
			case <-b.stopCh:
				return 0, fmt.Errorf("stopped")
			case <-ctx.Done():
				return 0, ctx.Err()
			}
		}
		if wait > 0 {
			select {
			case <-time.After(wait):
				continue
			case <-ctx.Done():
				return 0, ctx.Err()
			}
		}
	}
}

func (b *AdaptiveTokenBucket) tryConsume() (consumed bool, noToken bool, wait time.Duration, ruleID int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	curr := b.currTokens
	if curr == 0 {
		return false, true, 0, 0
	}
	ruleID = -1
	for _, rule := range b.consRules {
		if curr >= rule.Threshold {
			elapsed := time.Since(b.lastTs)
			wait = time.Until(b.nextProduceTs) / time.Duration(curr)
			if wait < 0 {
				wait = 0
			}
			if wait > rule.Wait {
				wait = rule.Wait
			}
			if elapsed < wait {
				return false, false, wait - elapsed, 0
			}
			ruleID = rule.RuleID
			break
		}
	}
	b.currTokens--
	b.lastTs = time.Now()
	return true, false, 0, ruleID
}

func (b *AdaptiveTokenBucket) Stop() {
	b.stopOnce.Do(func() {
		close(b.stopCh)
	})
}

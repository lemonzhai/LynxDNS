package rules

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
)

type RuleType string

const (
	TypeRemote   RuleType = "remote"
	TypeDomestic RuleType = "domestic"
	TypeRedirect RuleType = "redirect"
	TypeBlock    RuleType = "block"
)

type MatchMode string

const (
	MatchExact    MatchMode = "exact"
	MatchSuffix   MatchMode = "suffix"
	MatchWildcard MatchMode = "wildcard"
	MatchRegex    MatchMode = "regex"
	MatchKeyword  MatchMode = "keyword"
)

type Rule struct {
	ID        string    `json:"id"`
	Type      RuleType  `json:"type"`
	Domain    string    `json:"domain"`
	Target    string    `json:"target"`
	Enabled   *bool     `json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	matchMode MatchMode
	regex     *regexp.Regexp
	suffix    string
	keyword   string
}

type MatchResult struct {
	Matched     bool
	RuleType    RuleType
	Target      string
	RuleID      string
	RuleDomain  string
}

type Engine struct {
	mu    sync.RWMutex
	rules []*Rule
	nextID int
}

func NewEngine() *Engine {
	return &Engine{
		rules:  make([]*Rule, 0),
		nextID: 1,
	}
}

func boolPtr(b bool) *bool { return &b }

func boolVal(b *bool) bool {
	if b == nil {
		return false
	}
	return *b
}

func (e *Engine) Add(rtype RuleType, domain string, target string, enabled bool) (*Rule, error) {
	r := &Rule{
		ID:        fmt.Sprintf("rule_%03d", e.nextID),
		Type:      rtype,
		Domain:    domain,
		Target:    target,
		Enabled:   boolPtr(enabled),
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	if err := r.compile(); err != nil {
		return nil, err
	}

	e.mu.Lock()
	e.rules = append(e.rules, r)
	e.nextID++
	e.mu.Unlock()

	return r, nil
}

func (e *Engine) Update(id string, rtype RuleType, domain string, target string, enabled *bool) (*Rule, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	for _, r := range e.rules {
		if r.ID == id {
			if domain != "" {
				r.Domain = domain
			}
			if target != "" {
				r.Target = target
			}
			if rtype != "" {
				r.Type = rtype
			}
			if enabled != nil {
				r.Enabled = enabled
			}
			r.UpdatedAt = time.Now().UTC()

			if err := r.compile(); err != nil {
				return nil, err
			}
			return r, nil
		}
	}

	return nil, fmt.Errorf("rule not found: %s", id)
}

func (e *Engine) Delete(id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	for i, r := range e.rules {
		if r.ID == id {
			e.rules = append(e.rules[:i], e.rules[i+1:]...)
			return nil
		}
	}

	return fmt.Errorf("rule not found: %s", id)
}

func (e *Engine) Get(id string) (*Rule, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	for _, r := range e.rules {
		if r.ID == id {
			return r, true
		}
	}
	return nil, false
}

func (e *Engine) List(rtype RuleType, enabledOnly bool) []*Rule {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var result []*Rule
	for _, r := range e.rules {
		if rtype != "" && r.Type != rtype {
			continue
		}
		if enabledOnly && !boolVal(r.Enabled) {
			continue
		}
		result = append(result, r)
	}
	return result
}

func (e *Engine) Reorder(ids []string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	ruleMap := make(map[string]*Rule)
	for _, r := range e.rules {
		ruleMap[r.ID] = r
	}

	newRules := make([]*Rule, 0, len(ids))
	for _, id := range ids {
		if r, ok := ruleMap[id]; ok {
			newRules = append(newRules, r)
			delete(ruleMap, id)
		}
	}

	for _, r := range e.rules {
		if _, ok := ruleMap[r.ID]; ok {
			newRules = append(newRules, r)
		}
	}

	e.rules = newRules
	return nil
}

func (e *Engine) Match(domain string) *MatchResult {
	e.mu.RLock()
	defer e.mu.RUnlock()

	for _, r := range e.rules {
		if !boolVal(r.Enabled) {
			continue
		}
		if r.match(domain) {
			return &MatchResult{
				Matched:    true,
				RuleType:   r.Type,
				Target:     r.Target,
				RuleID:     r.ID,
				RuleDomain: r.Domain,
			}
		}
	}

	return &MatchResult{Matched: false}
}

func (r *Rule) compile() error {
	d := r.Domain

	switch {
	case strings.HasPrefix(d, "regexp:"):
		pattern := strings.TrimPrefix(d, "regexp:")
		re, err := regexp.Compile(pattern)
		if err != nil {
			return fmt.Errorf("invalid regex pattern: %s", pattern)
		}
		r.matchMode = MatchRegex
		r.regex = re

	case strings.HasPrefix(d, "keyword:"):
		r.matchMode = MatchKeyword
		r.keyword = strings.TrimPrefix(d, "keyword:")

	case strings.HasPrefix(d, "*."):
		r.matchMode = MatchWildcard
		r.suffix = d[1:]

	case strings.HasPrefix(d, "."):
		r.matchMode = MatchSuffix
		r.suffix = d

	default:
		r.matchMode = MatchExact
	}

	return nil
}

func (r *Rule) match(domain string) bool {
	domain = strings.ToLower(domain)
	target := strings.ToLower(r.Domain)

	switch r.matchMode {
	case MatchExact:
		return domain == target

	case MatchSuffix:
		return strings.HasSuffix(domain, r.suffix) || domain == target[1:]

	case MatchWildcard:
		return strings.HasSuffix(domain, r.suffix) || domain == target[2:]

	case MatchRegex:
		if r.regex != nil {
			return r.regex.MatchString(domain)
		}
		return false

	case MatchKeyword:
		return strings.Contains(domain, r.keyword)
	}

	return false
}

func (e *Engine) LoadFromConfig(rules []struct {
	Type    string `yaml:"type" json:"type"`
	Domain  string `yaml:"domain" json:"domain"`
	Target  string `yaml:"target" json:"target"`
	Enabled *bool  `yaml:"enabled" json:"enabled"`
}) error {
	for _, r := range rules {
		enabled := true
		if r.Enabled != nil {
			enabled = *r.Enabled
		}
		if _, err := e.Add(RuleType(r.Type), r.Domain, r.Target, enabled); err != nil {
			return fmt.Errorf("failed to load rule %s: %w", r.Domain, err)
		}
	}
	return nil
}

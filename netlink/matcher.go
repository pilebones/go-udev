package netlink

import (
	"regexp"
	"strings"
)

type Matcher interface {
	Evaluate(e UEvent) bool
	EvaluateAction(a KObjAction) bool
	EvaluateEnv(e map[string]string) bool
	Compile() error
	String() string
}

var (
	_ Matcher = (*RuleDefinition)(nil)
	_ Matcher = (*RuleDefinitions)(nil)
)

type RuleDefinition struct {
	Action *string           `json:"action,omitempty"`
	Env    map[string]string `json:"env,omitempty"`
	rule   *rule
}

// Evaluate return true if all condition match uevent and envs in rule exists in uevent
func (r *RuleDefinition) Evaluate(e UEvent) bool {
	// Compile if needed
	if r.rule == nil {
		if err := r.Compile(); err != nil {
			return false
		}
	}

	return r.EvaluateAction(e.Action) && r.EvaluateEnv(e.Env)
}

// EvaluateAction return true if the action match
func (r *RuleDefinition) EvaluateAction(a KObjAction) bool {
	// Compile if needed
	if r.rule == nil {
		if err := r.Compile(); err != nil {
			return false
		}
	}

	if r.rule.Action == nil {
		return true
	}

	return r.rule.Action.MatchString(a.String())
}

// EvaluateEnv return true if all env match and exists
func (r *RuleDefinition) EvaluateEnv(e map[string]string) bool {
	// Compile if needed
	if r.rule == nil {
		if err := r.Compile(); err != nil {
			return false
		}
	}
	return r.rule.Env.Evaluate(e)
}

// Compile prepare rule definition to be able to Evaluate() an UEvent
func (r *RuleDefinition) Compile() error {
	r.rule = &rule{
		Env: make(map[string]*regexp.Regexp),
	}

	if r.Action != nil {
		action, err := regexp.Compile(*(r.Action))
		if err != nil {
			return err
		}
		r.rule.Action = action
	}

	for k, v := range r.Env {
		reg, err := regexp.Compile(v)
		if err != nil {
			return err
		}
		r.rule.Env[k] = reg
	}
	return nil
}

func (r *RuleDefinition) String() string {
	b := strings.Builder{}
	b.WriteString("ruledef ( ")

	if r.Action == nil && len(r.Env) == 0 {
		b.WriteString("empty")
	} else {
		if r.Action != nil {
			b.WriteString("action=")
			b.WriteString(*r.Action)
			b.WriteRune(' ')
		}

		for k, v := range r.Env {
			b.WriteString("env.")
			b.WriteString(k)
			b.WriteRune('=')
			b.WriteString(v)
			b.WriteRune(' ')
		}
	}
	b.WriteString(")")
	return b.String()
}

// rule is the compiled version of the RuleDefinition
type rule struct {
	Action *regexp.Regexp
	Env    Env
}

type Env map[string]*regexp.Regexp

func (e Env) Evaluate(env map[string]string) bool {
	for envName, reg := range e {
		v, found := env[envName]
		if !found || !reg.MatchString(v) {
			return false
		}
	}

	return true
}

// RuleDefinitions is like chained rule with OR operator
type RuleDefinitions struct {
	Rules []RuleDefinition
}

func (rs *RuleDefinitions) AddRule(r RuleDefinition) {
	rs.Rules = append(rs.Rules, r)
}

// Compile prepare all rule definitions to be able to Evaluate() an UEvent.
// Rules are compiled in place, so the regexps are built once and not on each evaluation.
func (rs *RuleDefinitions) Compile() error {
	for i := range rs.Rules {
		if err := rs.Rules[i].Compile(); err != nil {
			return err
		}
	}
	return nil
}

func (rs *RuleDefinitions) Evaluate(e UEvent) bool {
	for i := range rs.Rules {
		if rs.Rules[i].Evaluate(e) {
			return true
		}
	}
	return false
}

// EvaluateAction return true if the action match
func (rs *RuleDefinitions) EvaluateAction(a KObjAction) bool {
	for i := range rs.Rules {
		if rs.Rules[i].EvaluateAction(a) {
			return true
		}
	}
	return false
}

// EvaluateEnv return true if almost one env match all regexp
func (rs *RuleDefinitions) EvaluateEnv(e map[string]string) bool {
	for i := range rs.Rules {
		if rs.Rules[i].EvaluateEnv(e) {
			return true
		}
	}
	return false
}

func (rs *RuleDefinitions) String() string {
	var output strings.Builder
	for i := range rs.Rules {
		output.WriteString("- " + rs.Rules[i].String() + "\n")
	}
	return output.String()
}

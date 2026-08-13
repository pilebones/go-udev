package netlink

import (
	"strings"
	"testing"
)

// TestRuleDefinitionsCompilePersist ensures Compile() keeps the compiled regexps, otherwise every
// evaluation recompiles them, which is both wasteful and hides invalid rules until the first event.
func TestRuleDefinitionsCompilePersist(t *testing.T) {
	action := ADD.String()
	rules := RuleDefinitions{Rules: []RuleDefinition{{
		Action: &action,
		Env:    map[string]string{"SUBSYSTEM": "usb"},
	}}}

	if err := rules.Compile(); err != nil {
		t.Fatalf("rules should compile, err: %v", err)
	}

	if rules.Rules[0].rule == nil {
		t.Fatal("Compile() must store the compiled rule in place")
	}

	// Evaluating must not replace the already compiled rule
	compiled := rules.Rules[0].rule
	rules.Evaluate(UEvent{Action: ADD, Env: map[string]string{"SUBSYSTEM": "usb"}})

	if rules.Rules[0].rule != compiled {
		t.Fatal("Evaluate() must reuse the compiled rule instead of recompiling it")
	}
}

// TestRuleDefinitionsAddRule ensures a rule set can be built incrementally, which is what a caller
// translating its own configuration into rules does.
func TestRuleDefinitionsAddRule(t *testing.T) {
	action := ADD.String()
	var rules RuleDefinitions

	rules.AddRule(RuleDefinition{Action: &action})
	rules.AddRule(RuleDefinition{Env: map[string]string{"SUBSYSTEM": "usb"}})

	if len(rules.Rules) != 2 {
		t.Fatalf("the set should hold both rules, got: %d", len(rules.Rules))
	}

	if !rules.Evaluate(UEvent{Action: ADD, Env: map[string]string{"SUBSYSTEM": "tty"}}) {
		t.Fatal("the rule on the action alone should match the event")
	}
}

// TestMatcherString ensures the printable form mentions every condition, since it is what the CLI shows to
// tell the user what it is filtering on.
func TestMatcherString(t *testing.T) {
	action := ADD.String()

	if got := (&RuleDefinition{}).String(); got != "ruledef ( empty)" {
		t.Fatalf("a rule without any condition should say so, got: %q", got)
	}

	rule := RuleDefinition{Action: &action, Env: map[string]string{"SUBSYSTEM": "usb"}}
	for _, want := range []string{"action=add", "env.SUBSYSTEM=usb"} {
		if got := rule.String(); !strings.Contains(got, want) {
			t.Fatalf("the rule should mention %q, got: %q", want, got)
		}
	}

	rules := RuleDefinitions{Rules: []RuleDefinition{rule, {}}}
	if got := strings.Count(rules.String(), "\n"); got != 2 {
		t.Fatalf("the set should print one line per rule, got %d lines for 2 rules", got)
	}
}

// TestRuleDefinitionCompilesOnDemand ensures every entry point compiles the rule when the caller did not,
// and that a rule which cannot compile refuses every event instead of accepting them all.
func TestRuleDefinitionCompilesOnDemand(t *testing.T) {
	action := ADD.String()
	event := UEvent{Action: ADD, Env: map[string]string{"SUBSYSTEM": "usb"}}

	// A fresh rule per call, otherwise only the first one goes through the compilation.
	matching := func() RuleDefinition {
		return RuleDefinition{Action: &action, Env: map[string]string{"SUBSYSTEM": "usb"}}
	}

	if r := matching(); !r.Evaluate(event) {
		t.Fatal("Evaluate should compile the rule instead of rejecting the event")
	}
	if r := matching(); !r.EvaluateAction(event.Action) {
		t.Fatal("EvaluateAction should compile the rule instead of rejecting the action")
	}
	if r := matching(); !r.EvaluateEnv(event.Env) {
		t.Fatal("EvaluateEnv should compile the rule instead of rejecting the env")
	}

	invalid := "(" // not a valid regexp
	broken := func() RuleDefinition {
		return RuleDefinition{Action: &invalid, Env: map[string]string{"SUBSYSTEM": invalid}}
	}

	if r := broken(); r.Evaluate(event) {
		t.Fatal("Evaluate should not match when the rule cannot compile")
	}
	if r := broken(); r.EvaluateAction(event.Action) {
		t.Fatal("EvaluateAction should not match when the rule cannot compile")
	}
	if r := broken(); r.EvaluateEnv(event.Env) {
		t.Fatal("EvaluateEnv should not match when the rule cannot compile")
	}
}

// TestRuleDefinitionsEvaluateAny ensures a set answers as soon as one of its rules accepts, the rules being
// chained with an OR, and that an empty set accepts nothing.
func TestRuleDefinitionsEvaluateAny(t *testing.T) {
	add := ADD.String()
	wrong := "can't match"

	rules := RuleDefinitions{Rules: []RuleDefinition{
		{Action: &wrong, Env: map[string]string{"SUBSYSTEM": "usb"}},
		{Action: &add, Env: map[string]string{"SUBSYSTEM": "tty"}},
	}}
	if err := rules.Compile(); err != nil {
		t.Fatalf("rules should compile, err: %v", err)
	}

	if !rules.EvaluateAction(ADD) {
		t.Fatal("the second rule accepts the action, so the set should")
	}
	if rules.EvaluateAction(REMOVE) {
		t.Fatal("no rule accepts the action, so the set should not")
	}
	if !rules.EvaluateEnv(map[string]string{"SUBSYSTEM": "tty"}) {
		t.Fatal("the second rule accepts the env, so the set should")
	}
	if rules.EvaluateEnv(map[string]string{"SUBSYSTEM": "block"}) {
		t.Fatal("no rule accepts the env, so the set should not")
	}

	var empty RuleDefinitions
	if empty.Evaluate(UEvent{Action: ADD}) || empty.EvaluateAction(ADD) || empty.EvaluateEnv(nil) {
		t.Fatal("a set without any rule should accept nothing")
	}
}

func TestRuleDefinitionsCompileInvalid(t *testing.T) {
	invalid := "(["
	rules := RuleDefinitions{Rules: []RuleDefinition{{Action: &invalid}}}

	if err := rules.Compile(); err == nil {
		t.Fatal("an invalid regexp must be reported by Compile()")
	}

	// The action is compiled first, so only a rule with a valid one reaches the env regexps.
	action := ADD.String()
	rules = RuleDefinitions{Rules: []RuleDefinition{{Action: &action, Env: map[string]string{"SUBSYSTEM": invalid}}}}

	if err := rules.Compile(); err == nil {
		t.Fatal("an invalid env regexp must be reported by Compile() too")
	}
}

func TestRules(testing *testing.T) {
	type testcase struct {
		object any
		valid  bool
	}

	t := testingWrapper{testing}

	// Given
	uevent := UEvent{
		Action: ADD,
		KObj:   "/devices/pci0000:00/0000:00:14.0/usb2/2-1/2-1:1.2/0003:04F2:0976.0008/hidraw/hidraw4",
		Env: map[string]string{
			"ACTION":    "add",
			"DEVPATH":   "/devices/pci0000:00/0000:00:14.0/usb2/2-1/2-1:1.2/0003:04F2:0976.0008/hidraw/hidraw4",
			"SUBSYSTEM": "hidraw",
			"MAJOR":     "247",
			"MINOR":     "4",
			"DEVNAME":   "hidraw4",
			"SEQNUM":    "2569",
		},
	}

	add := ADD.String()
	wrongAction := "can't match"

	// When
	rules := []RuleDefinition{
		{
			Action: nil,
			Env: map[string]string{
				"DEVNAME": "hidraw\\d+",
				"MAJOR":   "\\d+",
			},
		},

		{
			Action: &add,
			Env:    make(map[string]string),
		},

		{
			Action: nil,
			Env: map[string]string{
				"SUBSYSTEM": "can't match",
				"MAJOR":     "247",
			},
		},

		{
			Action: &add,
			Env: map[string]string{
				"SUBSYSTEM": "hidraw",
				"MAJOR":     "\\d+",
			},
		},

		{
			Action: &wrongAction,
			Env: map[string]string{
				"SUBSYSTEM": "hidraw",
				"MAJOR":     "\\d+",
			},
		},
		{
			Action: &add,
			Env: map[string]string{
				"DEVNAME": "hidraw\\d+",
				"MAJOR":   "247",
			},
		},
	}

	// Then
	testcases := []testcase{
		{
			object: &rules[0],
			valid:  true,
		},
		{
			object: &rules[1],
			valid:  true,
		},
		{
			object: &rules[2],
			valid:  false,
		},
		{
			object: &rules[3],
			valid:  true,
		},
		{
			object: &rules[4],
			valid:  false,
		},
		{
			object: &RuleDefinitions{[]RuleDefinition{rules[0], rules[4]}},
			valid:  true,
		},
		{
			object: &RuleDefinitions{[]RuleDefinition{rules[4], rules[0]}},
			valid:  true,
		},
		{
			object: &RuleDefinitions{[]RuleDefinition{rules[2], rules[4]}},
			valid:  false,
		},
		{
			object: &RuleDefinitions{[]RuleDefinition{rules[3], rules[1]}},
			valid:  true,
		},
		{
			object: &rules[5],
			valid:  true,
		},
	}

	for k, tcase := range testcases {
		matcher := tcase.object.(Matcher)

		err := matcher.Compile()
		t.FatalfIf(err != nil, "Testcase n°%d should compile without error, err: %v", k+1, err)

		ok := matcher.Evaluate(uevent)
		t.FatalfIf((ok != tcase.valid) && tcase.valid, "Testcase n°%d should evaluate event", k+1)
		t.FatalfIf((ok != tcase.valid) && !tcase.valid, "Testcase n°%d shouldn't evaluate event", k+1)
	}
}

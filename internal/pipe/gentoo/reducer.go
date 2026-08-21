package gentoo

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
)

type installStmt interface {
	isInstallStmt()
	String(indent string) string
	Validate() error
	Equals(other installStmt) bool
}

type stateStmt struct {
	Family StateFamily
	Value  string
}

func (s stateStmt) isInstallStmt() {}

func (s stateStmt) String(indent string) string {
	var sb strings.Builder
	sb.WriteString(indent)
	sb.WriteString(string(s.Family))
	if s.Value != "" {
		sb.WriteString(" ")
		sb.WriteString(s.Value)
	}
	sb.WriteString("\n")
	return sb.String()
}

func (s stateStmt) Validate() error {
	if s.Family == "" {
		return errors.New("stateStmt requires a state family")
	}
	return nil
}

func (s stateStmt) Equals(other installStmt) bool {
	o, ok := other.(stateStmt)
	return ok && s.Family == o.Family && s.Value == o.Value
}

type actionStmt struct {
	Op            InstallOp
	Source        string
	Target        string
	Die           string
	RequiredState StateRequirement
}

func (a actionStmt) isInstallStmt() {}

func (a actionStmt) String(indent string) string {
	desc := a.Op.Descriptor()
	var sb strings.Builder
	sb.WriteString(indent)
	sb.WriteString(desc.Command)
	sb.WriteString(" \"")
	sb.WriteString(a.Source)
	sb.WriteString("\"")

	if desc.TakesTwoArgs() && a.Target != "" {
		sb.WriteString(" \"")
		sb.WriteString(a.Target)
		sb.WriteString("\"")
	}

	if desc.AppendDie {
		dieMsg := a.Die
		if dieMsg == "" {
			if a.Target != "" {
				dieMsg = "Failed to install " + a.Target
			} else {
				dieMsg = "Failed to install " + a.Source
			}
		}
		sb.WriteString(" || die \"")
		sb.WriteString(dieMsg)
		sb.WriteString("\"")
	}
	sb.WriteString("\n")
	return sb.String()
}

func (a actionStmt) Validate() error {
	desc := a.Op.Descriptor()
	if a.Source == "" {
		return fmt.Errorf("%s requires a source argument", desc.Command)
	}
	if a.Op == OpDosym && a.Target == "" {
		return errors.New("dosym requires a destination")
	}
	if desc.IsRename && a.Target == "" {
		return fmt.Errorf("%s requires a destination", a.Op)
	}
	if desc.ArgMode == ArgModeSingle && a.Target != "" {
		return fmt.Errorf("%s does not accept a second argument", desc.Command)
	}
	if desc.StateFamily != StateFamilyNone {
		if a.RequiredState.Family != desc.StateFamily {
			return fmt.Errorf("%s requires state family %s, got %s", desc.Command, desc.StateFamily, a.RequiredState.Family)
		}
	} else {
		if a.RequiredState.Family != StateFamilyNone {
			return fmt.Errorf("%s does not use destination state, got family %s", desc.Command, a.RequiredState.Family)
		}
	}
	return nil
}

func (a actionStmt) Equals(other installStmt) bool {
	o, ok := other.(actionStmt)
	return ok && a.Op == o.Op && a.Source == o.Source && a.Target == o.Target && a.Die == o.Die && a.RequiredState == o.RequiredState
}

type rawStmt struct {
	Content string
}

func (r rawStmt) isInstallStmt() {}

func (r rawStmt) String(indent string) string {
	if r.Content == "" {
		return ""
	}
	var sb strings.Builder
	for line := range strings.SplitSeq(r.Content, "\n") {
		if line != "" {
			sb.WriteString(indent)
			sb.WriteString(line)
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func (r rawStmt) Validate() error { return nil }

func (r rawStmt) Equals(other installStmt) bool {
	o, ok := other.(rawStmt)
	return ok && r.Content == o.Content
}

type conditionStmt struct {
	Expr conditionExpr
	Body []installStmt
}

func (c conditionStmt) isInstallStmt() {}

func (c conditionStmt) String(indent string) string {
	var sb strings.Builder
	sb.WriteString(indent)
	sb.WriteString("if ")
	if c.Expr != nil {
		sb.WriteString(c.Expr.Shell())
	}
	sb.WriteString("; then\n")
	for _, stmt := range c.Body {
		sb.WriteString(stmt.String(indent + "  "))
	}
	sb.WriteString(indent)
	sb.WriteString("fi\n")
	return sb.String()
}

func (c conditionStmt) Validate() error {
	if c.Expr == nil {
		return errors.New("condition requires an expression")
	}
	for _, s := range c.Body {
		if err := s.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func (c conditionStmt) Equals(other installStmt) bool {
	o, ok := other.(conditionStmt)
	if !ok {
		return false
	}
	if (c.Expr == nil) != (o.Expr == nil) {
		return false
	}
	if c.Expr != nil && !c.Expr.Equals(o.Expr) {
		return false
	}
	if len(c.Body) != len(o.Body) {
		return false
	}
	for i := range c.Body {
		if !c.Body[i].Equals(o.Body[i]) {
			return false
		}
	}
	return true
}

// InstallProgram is the install statement IR shared by lowering, reduction,
// validation, and ebuild rendering.
type InstallProgram struct {
	UniverseArchitectures []string
	Body                  []installStmt
}

func (p *InstallProgram) String(indent string) string {
	if p == nil {
		return ""
	}
	return formatStmts(p.Body, indent)
}

// Reduce simplifies an install program to a fixed point while preserving its
// state requirements and architecture/USE conditions.
func (p *InstallProgram) Reduce() *InstallProgram {
	if p == nil {
		return nil
	}
	return (&installReducer{universe: p.UniverseArchitectures}).reduceFixedPoint(p)
}

type installReducer struct {
	universe []string
}

type installState map[StateFamily]string

func newInstallState() installState { return installState(InitialState()) }

func (s installState) Clone() installState {
	clone := installState{}
	maps.Copy(clone, s)
	return clone
}

func (s installState) Get(family StateFamily) (string, bool) {
	value, ok := s[family]
	return value, ok
}
func (s installState) Set(family StateFamily, value string) { s[family] = value }
func (s installState) Clear()                               { clear(s) }

func (s installState) Join(before, branch installState) {
	for family, value := range before {
		branchValue, ok := branch[family]
		if !ok || branchValue != value {
			delete(s, family)
		}
	}
	for family := range branch {
		if _, ok := before[family]; !ok {
			delete(s, family)
		}
	}
}

func (r *installReducer) reduceFixedPoint(plan *InstallProgram) *InstallProgram {
	current := plan
	const maxIterations = 100
	for range maxIterations {
		next := &InstallProgram{
			UniverseArchitectures: current.UniverseArchitectures,
			Body:                  r.reduceStatements(current.Body, newInstallState()),
		}
		if planEqual(current, next) {
			return next
		}
		current = next
	}
	panic(fmt.Sprintf("reducer failed to converge after %d iterations", maxIterations))
}

func (p *InstallProgram) Validate() error {
	if p == nil {
		return nil
	}
	for _, stmt := range p.Body {
		if err := stmt.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func planEqual(a, b *InstallProgram) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if !slices.Equal(a.UniverseArchitectures, b.UniverseArchitectures) {
		return false
	}
	if len(a.Body) != len(b.Body) {
		return false
	}
	for i := range a.Body {
		if !a.Body[i].Equals(b.Body[i]) {
			return false
		}
	}
	return true
}

func (r *installReducer) reduceStatements(stmts []installStmt, state installState) []installStmt {
	var reduced []installStmt

	for _, stmt := range stmts {
		switch s := stmt.(type) {
		case conditionStmt:
			simplifiedExpr := simplifyExpr(s.Expr, r.universe)
			if simplifiedExpr == nil || simplifiedExpr.Equals(TrueExpr{}) {
				sBody := r.reduceStatements(s.Body, state)
				reduced = append(reduced, sBody...)
				continue
			}
			if simplifiedExpr.Equals(FalseExpr{}) {
				continue
			}
			s.Expr = simplifiedExpr

			stateBefore := state.Clone()
			branchState := stateBefore.Clone()
			reducedBody := r.reduceStatements(s.Body, branchState)
			if len(reducedBody) == 0 {
				continue
			}

			s.Body = reducedBody
			reduced = append(reduced, s)

			state.Join(stateBefore, branchState)

		case rawStmt:
			state.Clear()
			reduced = append(reduced, s)

		case stateStmt:
			if val, ok := state.Get(s.Family); ok && val == s.Value {
				continue // redundant with already tracked state
			}
			state.Set(s.Family, s.Value)
			reduced = append(reduced, s)

		case actionStmt:
			if s.RequiredState.Family != StateFamilyNone {
				current, known := state.Get(s.RequiredState.Family)
				if !known || current != s.RequiredState.Value {
					reduced = append(reduced, stateStmt{
						Family: s.RequiredState.Family,
						Value:  s.RequiredState.Value,
					})
					state.Set(s.RequiredState.Family, s.RequiredState.Value)
				}
			}
			reduced = append(reduced, s)

		default:
			reduced = append(reduced, s)
		}
	}

	// Merge and factor sibling conditions
	reduced = r.mergeSiblingConditions(reduced)

	return reduced
}

func (r *installReducer) mergeSiblingConditions(stmts []installStmt) []installStmt {
	var result []installStmt
	i := 0
	for i < len(stmts) {
		_, isCond := stmts[i].(conditionStmt)
		if !isCond {
			if raw, isRaw := stmts[i].(rawStmt); isRaw && raw.Content == "" {
				i++
				continue
			}
			result = append(result, stmts[i])
			i++
			continue
		}

		// Collect contiguous conditionStmts
		var conds []conditionStmt
		j := i
		for j < len(stmts) {
			c, ok := stmts[j].(conditionStmt)
			if !ok {
				break
			}
			conds = append(conds, c)
			j++
		}

		factored := r.factorConditions(conds)
		result = append(result, factored...)
		i = j
	}
	return result
}

func (r *installReducer) factorConditions(conds []conditionStmt) []installStmt {
	if len(conds) == 0 {
		return nil
	}
	if len(conds) == 1 {
		return []installStmt{conds[0]}
	}

	var result []installStmt
	i := 0
	for i < len(conds) {
		bestJ := i
		var bestFactor conditionExpr

		for j := len(conds) - 1; j > i; j-- {
			factors := findCommonFactors(conds[i : j+1])
			if len(factors) > 0 {
				bestJ = j
				bestFactor = selectBestFactor(factors)
				break
			}
		}

		if bestJ > i && bestFactor != nil {
			var innerBody []installStmt
			for k := i; k <= bestJ; k++ {
				rem := removeTerm(conds[k].Expr, bestFactor)
				if rem == nil || rem.Equals(TrueExpr{}) {
					innerBody = append(innerBody, conds[k].Body...)
				} else {
					innerBody = append(innerBody, conditionStmt{
						Expr: rem,
						Body: conds[k].Body,
					})
				}
			}
			factoredCond := conditionStmt{
				Expr: bestFactor,
				Body: r.mergeSiblingConditions(innerBody),
			}
			result = append(result, factoredCond)
			i = bestJ + 1
		} else {
			result = append(result, conds[i])
			i++
		}
	}
	return result
}

func getTerms(expr conditionExpr) []conditionExpr {
	if expr == nil {
		return nil
	}
	if and, ok := expr.(AndExpr); ok {
		return and.Exprs
	}
	return []conditionExpr{expr}
}

func findCommonFactors(slice []conditionStmt) []conditionExpr {
	if len(slice) == 0 {
		return nil
	}
	common := getTerms(slice[0].Expr)
	for _, c := range slice[1:] {
		cTerms := getTerms(c.Expr)
		var nextCommon []conditionExpr
		for _, f := range common {
			if slices.ContainsFunc(cTerms, func(ct conditionExpr) bool {
				return f.Equals(ct)
			}) {
				nextCommon = append(nextCommon, f)
			}
		}
		common = nextCommon
		if len(common) == 0 {
			break
		}
	}
	return common
}

func selectBestFactor(factors []conditionExpr) conditionExpr {
	if len(factors) == 0 {
		return nil
	}
	slices.SortFunc(factors, func(a, b conditionExpr) int {
		pA := factorPriority(a)
		pB := factorPriority(b)
		if pA != pB {
			return pA - pB
		}
		return strings.Compare(a.String(), b.String())
	})
	return factors[0]
}

func factorPriority(e conditionExpr) int {
	switch e.(type) {
	case ArchExpr, OrExpr:
		return 1
	case UseExpr:
		return 2
	case NotExpr:
		return 3
	default:
		return 4
	}
}

func removeTerm(expr conditionExpr, term conditionExpr) conditionExpr {
	if expr == nil || term == nil {
		return expr
	}
	if expr.Equals(term) {
		return TrueExpr{}
	}
	if and, ok := expr.(AndExpr); ok {
		var remaining []conditionExpr
		for _, t := range and.Exprs {
			if !t.Equals(term) {
				remaining = append(remaining, t)
			}
		}
		if len(remaining) == 0 {
			return TrueExpr{}
		}
		if len(remaining) == 1 {
			return remaining[0]
		}
		return NewAndExpr(remaining...)
	}
	return expr
}

func formatStmts(stmts []installStmt, indent string) string {
	var sb strings.Builder
	for _, stmt := range stmts {
		sb.WriteString(stmt.String(indent))
	}
	return strings.TrimRight(sb.String(), "\n")
}

package refusalscan

import (
	stderrors "errors"
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"sort"
)

// shapeChecker admits direct propagation and scoped conversion to non-error
// outcomes. Objects, rather than identifier spelling, connect init bindings
// to uses. Error aliases, storage and closures are never conversion outcomes.
type shapeChecker struct {
	errors, errorTypes, helpers symbolSet
	allowed                     map[token.Pos]bool
	parents                     map[ast.Node]ast.Node
	results                     *ast.FieldList
	bound                       symbolSet
}

func validateRefusalUseShapes(files map[string]*parsedSource, errors, errorTypes, helpers symbolSet) error {
	var paths []string
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	var failures []error
	for _, path := range paths {
		source := files[path]
		c := shapeChecker{errors: errors, errorTypes: errorTypes, helpers: helpers, allowed: make(map[token.Pos]bool), parents: make(map[ast.Node]ast.Node), bound: newSymbolSet(errors.info)}
		var stack []ast.Node
		ast.Inspect(source.file, func(n ast.Node) bool {
			if n == nil {
				stack = stack[:len(stack)-1]
				return true
			}
			if len(stack) > 0 {
				c.parents[n] = stack[len(stack)-1]
			}
			stack = append(stack, n)
			return true
		})
		for _, declaration := range source.file.Decls {
			switch d := declaration.(type) {
			case *ast.FuncDecl:
				if d.Body == nil {
					continue
				}
				if errorImplementation(d, errors.info) {
					c.mark(d.Body, errors, newSymbolSet(errors.info), newSymbolSet(errors.info))
				}
				c.function(d.Body, d.Type.Results)
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					if v, ok := spec.(*ast.ValueSpec); ok && sentinelDeclaration(v, errors) {
						// A sentinel declaration seeds identity, but cannot alias another
						// refusal or use a helper to store an error globally.
						for _, value := range v.Values {
							if !containsTypedRefusal(value, errors, newSymbolSet(errors.info), helpers) {
								c.mark(value, errors, errorTypes, helpers)
							}
						}
					}
				}
			}
		}
		var forbidden token.Pos
		ast.Inspect(source.file, func(n ast.Node) bool {
			if forbidden.IsValid() {
				return false
			}
			// Declarations and type syntax are not value uses.
			if id, ok := n.(*ast.Ident); ok && errors.info.Defs[id] != nil {
				return true
			}
			if pos, ok := typedRefusalReference(n, errors, errorTypes, helpers); ok && !c.allowed[pos] {
				forbidden = pos
				return false
			}
			return true
		})
		if forbidden.IsValid() {
			failures = append(failures, fmt.Errorf("refusalscan: typed refusal use outside a return expression or refusal if-init (scoped consumption) at %s:%d", path, source.fset.Position(forbidden).Line))
		}
	}
	return stderrors.Join(failures...)
}

func (c *shapeChecker) mark(n ast.Node, errors, errorTypes, helpers symbolSet) {
	markRefusalReferences(c.allowed, n, errors, errorTypes, helpers)
}
func (c *shapeChecker) tainted(e ast.Expr) bool {
	return containsTypedRefusal(e, c.errors, c.errorTypes, c.helpers) || c.containsBound(e)
}
func (c *shapeChecker) containsBound(n ast.Node) bool {
	found := false
	ast.Inspect(n, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && c.bound.has(id) {
			found = true
		}
		return !found
	})
	return found
}
func (c *shapeChecker) function(body *ast.BlockStmt, results *ast.FieldList) {
	previousResults, previousBound := c.results, c.bound
	c.results = results
	c.bound = newSymbolSet(c.errors.info)
	defer func() { c.results = previousResults; c.bound = previousBound }()
	ast.Inspect(body, func(n ast.Node) bool {
		if literal, ok := n.(*ast.FuncLit); ok {
			c.function(literal.Body, literal.Type.Results)
			return false
		}
		switch s := n.(type) {
		case *ast.IfStmt:
			c.init(s.Init, s)
			if c.consumption(s.Cond) {
				c.mark(s.Cond, c.errors, c.errorTypes, c.helpers)
			}
		case *ast.SwitchStmt:
			c.init(s.Init, s)
			if s.Tag != nil && c.consumption(s.Tag) {
				c.mark(s.Tag, c.errors, c.errorTypes, c.helpers)
			}
		case *ast.CaseClause:
			for _, e := range s.List {
				if c.consumption(e) {
					c.mark(e, c.errors, c.errorTypes, c.helpers)
				}
			}
		case *ast.ReturnStmt:
			for i, e := range s.Results {
				if c.propagation(e) && (refusalResultType(results, i, c.errorTypes) || (len(s.Results) == 1 && resultFieldCount(results) > 1 && c.helperCall(e))) {
					c.mark(e, c.errors, c.errorTypes, c.helpers)
				} else if c.consumption(e) {
					c.mark(e, c.errors, c.errorTypes, c.helpers)
				}
			}
		case *ast.ExprStmt:
			if c.consumption(s.X) || c.terminalPanic(s) {
				c.mark(s.X, c.errors, c.errorTypes, c.helpers)
			}
		case *ast.AssignStmt:
			// Only conversions may survive the statement. Raw errors and `_`
			// discards cannot. Field/container assignments cannot retain errors.
			for i, e := range s.Rhs {
				if c.consumption(e) && !c.discard(s, i) {
					if c.convertedBindingsSafe(body, s, i) {
						c.mark(e, c.errors, c.errorTypes, c.helpers)
					}
				}
			}
		}

		return true
	})
}
func (c *shapeChecker) discard(s *ast.AssignStmt, i int) bool {
	if len(s.Rhs) == 1 && len(s.Lhs) > 1 {
		for _, e := range s.Lhs {
			if id, ok := e.(*ast.Ident); ok && id.Name == "_" {
				return true
			}
		}
		return false
	}
	return i < len(s.Lhs) && isBlank(s.Lhs[i])
}
func isBlank(e ast.Expr) bool { id, ok := e.(*ast.Ident); return ok && id.Name == "_" }
func (c *shapeChecker) helperCall(e ast.Expr) bool {
	call, ok := e.(*ast.CallExpr)
	return ok && c.helpers.has(call.Fun)
}
func (c *shapeChecker) propagation(e ast.Expr) bool {
	switch e := e.(type) {
	case *ast.Ident, *ast.SelectorExpr:
		return c.errors.has(e) || c.bound.has(e)
	case *ast.ParenExpr:
		return c.propagation(e.X)
	case *ast.UnaryExpr:
		return e.Op == token.AND && c.propagation(e.X)
	case *ast.CompositeLit:
		return c.errorTypes.has(e.Type) && !c.taintedElements(e.Elts)
	case *ast.CallExpr:
		if c.helperCall(e) || c.errorTypes.has(e.Fun) {
			for _, arg := range e.Args {
				if c.tainted(arg) && !c.propagation(arg) {
					return false
				}
			}
			return true
		}
		object := calledObject(e, c.errors.info)
		if object != nil && object.Pkg() != nil && object.Pkg().Path() == "fmt" && object.Name() == "Errorf" {
			if len(e.Args) == 0 {
				return false
			}
			format, ok := c.errors.info.Types[e.Args[0]]
			if !ok || format.Value == nil {
				return false
			}
			// fmt.Errorf must actually wrap; string formatting is consumption.
			if !hasWrappingVerb(constant.StringVal(format.Value)) {
				return false
			}
			wrapped := wrappingArguments(constant.StringVal(format.Value))
			for i, arg := range e.Args[1:] {
				if c.tainted(arg) && (!wrapped[i] || !c.propagation(arg)) {
					return false
				}
			}
			return c.tainted(e)
		}
		if object != nil && object.Pkg() != nil && object.Pkg().Path() == "errors" && object.Name() == "Join" {
			for _, arg := range e.Args {
				if c.tainted(arg) && !c.propagation(arg) {
					return false
				}
			}
			return c.tainted(e)
		}
	}
	return false
}
func (c *shapeChecker) taintedElements(elements []ast.Expr) bool {
	for _, e := range elements {
		if c.tainted(e) {
			return true
		}
	}
	return false
}
func (c *shapeChecker) consumption(e ast.Expr) bool {
	if !c.tainted(e) {
		return true
	}
	switch e := e.(type) {
	case *ast.ParenExpr:
		return c.consumption(e.X)
	case *ast.BinaryExpr:
		if classificationExpression(e, c.errors.info) {
			return c.comparisonOperand(e.X) && c.comparisonOperand(e.Y)
		}
		return c.consumption(e.X) && c.consumption(e.Y)
	case *ast.UnaryExpr:
		return e.Op == token.NOT && c.consumption(e.X)
	case *ast.KeyValueExpr:
		return c.consumption(e.Key) && c.consumption(e.Value)
	case *ast.CompositeLit:
		for _, el := range e.Elts {
			if !c.consumption(el) {
				return false
			}
		}
		return !implementsError(c.errors.info.TypeOf(e))
	case *ast.CallExpr:
		object := calledObject(e, c.errors.info)
		if c.errors.info.Types[e.Fun].IsType() {
			for _, arg := range e.Args {
				if !c.consumption(arg) {
					return false
				}
			}
			return true
		}
		if object == types.Universe.Lookup("panic") {
			return false
		}
		if noResults(c.errors.info.TypeOf(e)) && (object == nil || (object != types.Universe.Lookup("print") && object != types.Universe.Lookup("println") && !c.refusalConsumer(e))) {
			return false
		}
		if c.helperCall(e) || c.errorTypes.has(e.Fun) || hasErrorType(c.errors.info.TypeOf(e)) {
			return false
		}
		// Non-error result types do not prove that a call cannot retain its
		// arguments. Raw refusal arguments require a known consuming API.
		if sel, ok := e.Fun.(*ast.SelectorExpr); ok && c.tainted(sel.X) {
			if !c.containsBound(sel.X) || object == nil || object.Name() != "Error" || !types.Identical(c.errors.info.TypeOf(e), types.Typ[types.String]) {
				return false
			}
		} else if c.tainted(e.Fun) {
			return false
		}
		for _, arg := range e.Args {
			if !c.tainted(arg) {
				continue
			}
			if c.propagation(arg) {
				if !c.refusalConsumer(e) {
					return false
				}
			} else if !c.consumption(arg) {
				return false
			}
		}
		return true
	}
	return false
}

// Comparisons may classify a raw refusal, but both operands must obey the
// same closed consumer contract. A bool-returning call is not inherently safe.
func (c *shapeChecker) comparisonOperand(e ast.Expr) bool {
	return c.propagation(e) || c.consumption(e)
}

// The closed argument contract is intraprocedural: an arbitrary function's
// signature cannot establish that it converts rather than stores an error.
func (c *shapeChecker) refusalConsumer(call *ast.CallExpr) bool {
	object := calledObject(call, c.errors.info)
	if object == nil || object.Pkg() == nil {
		return false
	}
	switch object.Pkg().Path() {
	case "errors":
		return object.Name() == "Is" || object.Name() == "As"
	case "fmt":
		switch object.Name() {
		case "Sprint", "Sprintln":
			return true
		case "Sprintf":
			if len(call.Args) == 0 {
				return false
			}
			format := c.errors.info.Types[call.Args[0]].Value
			return format != nil && format.Kind() == constant.String && !hasWrappingVerb(constant.StringVal(format))
		}
	case "log", "log/slog":
		// Log constructors can retain their arguments. Only output calls
		// belong to the log/string conversion contract.
		switch object.Name() {
		case "Print", "Printf", "Println", "Fatal", "Fatalf", "Fatalln", "Panic", "Panicf", "Panicln", "Output",
			"Debug", "DebugContext", "Info", "InfoContext", "Warn", "WarnContext", "Error", "ErrorContext", "Log", "LogAttrs":
			return true
		}
	}
	return false
}
func hasErrorType(t types.Type) bool {
	if tuple, ok := t.(*types.Tuple); ok {
		for i := 0; i < tuple.Len(); i++ {
			if implementsError(tuple.At(i).Type()) {
				return true
			}
		}
		return false
	}
	return implementsError(t)
}
func calledObject(call *ast.CallExpr, info *types.Info) types.Object {
	function := call.Fun
	for {
		paren, ok := function.(*ast.ParenExpr)
		if !ok {
			break
		}
		function = paren.X
	}
	switch f := function.(type) {
	case *ast.Ident:
		return info.ObjectOf(f)
	case *ast.SelectorExpr:
		return info.ObjectOf(f.Sel)
	}
	return nil
}
func hasWrappingVerb(format string) bool {
	return len(wrappingArguments(format)) != 0
}
func (c *shapeChecker) terminalPanic(s *ast.ExprStmt) bool {
	call, ok := s.X.(*ast.CallExpr)
	if !ok || calledObject(call, c.errors.info) != types.Universe.Lookup("panic") || len(call.Args) != 1 || hasErrorResult(c.results, c.errorTypes) {
		return false
	}
	block, ok := c.parents[s].(*ast.BlockStmt)
	return ok && len(block.List) > 0 && block.List[len(block.List)-1] == s && (c.propagation(call.Args[0]) || c.consumption(call.Args[0]))
}
func hasErrorResult(results *ast.FieldList, symbols symbolSet) bool {
	if results != nil {
		for _, f := range results.List {
			if errorResultType(f.Type, symbols) {
				return true
			}
		}
	}
	return false
}

func (c *shapeChecker) init(init ast.Stmt, scope ast.Node) {
	assignment, ok := init.(*ast.AssignStmt)
	if !ok || assignment.Tok != token.DEFINE {
		return
	}
	// Admit a shared RHS only after EVERY error result is safely consumed.
	// In a tuple, TypeOf(_) is not the result type; inspect the RHS tuple.
	safeRHS := make(map[int]bool)
	bindings := make(map[int][]types.Object)
	for i, left := range assignment.Lhs {
		ri := i
		if len(assignment.Rhs) == 1 {
			ri = 0
		}
		if ri >= len(assignment.Rhs) || !c.tainted(assignment.Rhs[ri]) {
			continue
		}
		resultType := c.errors.info.TypeOf(assignment.Rhs[ri])
		if tuple, ok := resultType.(*types.Tuple); ok {
			if i >= tuple.Len() {
				continue
			}
			resultType = tuple.At(i).Type()
		}
		if !implementsError(resultType) {
			continue
		}
		if _, seen := safeRHS[ri]; !seen {
			safeRHS[ri] = true
		}
		id, ok := left.(*ast.Ident)
		if !ok || isBlank(left) || c.errors.info.Defs[id] == nil {
			safeRHS[ri] = false
			continue
		}
		obj := c.errors.info.ObjectOf(id)
		bindings[ri] = append(bindings[ri], obj)
		c.bound.objects[obj] = true
	}
	for ri, objects := range bindings {
		for _, obj := range objects {
			if !c.safeUses(scope, assignment, obj) {
				safeRHS[ri] = false
			}
		}
	}
	for _, objects := range bindings {
		for _, obj := range objects {
			delete(c.bound.objects, obj)
		}
	}
	for ri, safe := range safeRHS {
		if safe {
			c.mark(assignment.Rhs[ri], c.errors, c.errorTypes, c.helpers)
		}
	}
}
func (c *shapeChecker) safeUses(scope ast.Node, init *ast.AssignStmt, obj types.Object) bool {
	safe := true
	_, returningIf := scope.(*ast.IfStmt)
	ast.Inspect(scope, func(n ast.Node) bool {
		if _, ok := n.(*ast.FuncLit); ok {
			if c.containsBound(n) {
				safe = false
			}
			return false
		}
		id, ok := n.(*ast.Ident)
		if !ok || c.errors.info.ObjectOf(id) != obj || c.errors.info.Defs[id] != nil {
			return true
		}
		// A use must reach a consuming expression or an error-result return
		// without crossing storage, another binding, or a bare return.
		for cur := ast.Node(id); cur != scope; cur = c.parents[cur] {
			if cur == nil {
				safe = false
				break
			}

			switch s := c.parents[cur].(type) {
			case *ast.ReturnStmt:
				allowed := false
				for i, e := range s.Results {
					if e == cur && (c.consumption(e) || returningIf && refusalResultType(c.results, i, c.errorTypes) && c.propagation(e)) {
						allowed = true
					}
				}
				if !allowed {
					safe = false
				}
				return true
			case *ast.ExprStmt:
				if !c.terminalPanic(s) && !c.consumption(s.X) {
					safe = false
				}
				return true
			case *ast.AssignStmt:
				if s != init {
					allowed := false
					for i, e := range s.Rhs {
						if e == cur && c.consumption(e) && !c.discard(s, i) {
							allowed = c.convertedBindingsSafe(c.functionScope(s), s, i)
						}
					}
					if !allowed {
						safe = false
					}
				}
				return true
			case *ast.IfStmt:
				if s.Cond == cur && c.consumption(s.Cond) {
					return true
				}
			case *ast.SwitchStmt:
				if s.Tag == cur && c.consumption(s.Tag) {
					return true
				}
			case *ast.CaseClause:
				for _, e := range s.List {
					if e == cur && c.consumption(e) {
						return true
					}
				}
			case *ast.ValueSpec, *ast.SendStmt, *ast.IncDecStmt:
				safe = false
				return true
			}
		}
		return true
	})
	// Named-result bare returns cannot carry an init binding; assignments to
	// an outer result are rejected above, even in a returning scope.
	return safe
}
func classificationExpression(expression ast.Expr, info *types.Info) bool {
	if !types.Identical(info.TypeOf(expression), types.Typ[types.Bool]) {
		return false
	}
	switch e := expression.(type) {
	case *ast.BinaryExpr:
		return e.Op == token.EQL || e.Op == token.NEQ
	case *ast.CallExpr:
		obj := calledObject(e, info)
		return obj != nil && obj.Pkg() != nil && obj.Pkg().Path() == "errors" && (obj.Name() == "Is" || obj.Name() == "As")
	}
	return false
}

func noResults(t types.Type) bool {
	tuple, ok := t.(*types.Tuple)
	return t == nil || ok && tuple.Len() == 0
}

// Track fmt's argument cursor, including explicit indices and width/precision
// stars. Only a %w consuming the refusal argument establishes propagation.
func wrappingArguments(format string) map[int]bool {
	wrapped := make(map[int]bool)
	next := 0
	for i := 0; i < len(format); i++ {
		if format[i] != '%' {
			continue
		}
		i++
		if i >= len(format) {
			break
		}
		if format[i] == '%' {
			continue
		}
		for i < len(format) {
			ch := format[i]
			if ch == '[' {
				end := i + 1
				index := 0
				for end < len(format) && format[end] >= '0' && format[end] <= '9' {
					index = index*10 + int(format[end]-'0')
					end++
				}
				if end >= len(format) || format[end] != ']' || end == i+1 || index <= 0 {
					return map[int]bool{}
				}
				next = index - 1
				i = end + 1
				continue
			}
			if ch == '*' {
				next++
				i++
				continue
			}
			if ch == '+' || ch == '-' || ch == '#' || ch == ' ' || ch == '0' || ch == '.' || ch >= '1' && ch <= '9' {
				i++
				continue
			}
			if ch == 'w' {
				wrapped[next] = true
			}
			next++
			break
		}
	}
	return wrapped
}

// A non-error outcome may survive its consuming statement, but its object
// cannot then be converted back into an error. Follow local value bindings
// by identity; aggregate and indirect writes are rejected structurally.
func (c *shapeChecker) functionScope(n ast.Node) ast.Node {
	for n != nil {
		switch f := n.(type) {
		case *ast.FuncDecl:
			return f.Body
		case *ast.FuncLit:
			return f.Body
		}
		n = c.parents[n]
	}
	return nil
}
func (c *shapeChecker) convertedBindingsSafe(scope ast.Node, assignment *ast.AssignStmt, rhs int) bool {
	if !c.tainted(assignment.Rhs[rhs]) {
		return true
	}
	visited := make(map[types.Object]bool)
	for i, left := range assignment.Lhs {
		if len(assignment.Rhs) != 1 && i != rhs {
			continue
		}
		if !c.convertedTargetSafe(scope, left, visited) {
			return false
		}
	}
	return true
}

// Refusal-derived outcomes may survive as local values, but must never be
// assigned through aggregate or indirect targets. Rejecting the write itself
// covers aliases created before it without attempting heap/alias analysis.
func (c *shapeChecker) convertedTargetSafe(scope ast.Node, target ast.Expr, visited map[types.Object]bool) bool {
	switch target := target.(type) {
	case *ast.Ident:
		return !implementsError(c.errors.info.TypeOf(target)) && c.convertedObjectSafe(scope, c.errors.info.ObjectOf(target), visited)
	case *ast.SelectorExpr:
		return false
	case *ast.IndexExpr:
		return false
	case *ast.StarExpr:
		return false
	case *ast.ParenExpr:
		return c.convertedTargetSafe(scope, target.X, visited)
	}
	return false
}
func (c *shapeChecker) convertedObjectSafe(scope ast.Node, object types.Object, visited map[types.Object]bool) bool {
	if object == nil || visited[object] {
		return true
	}
	visited[object] = true
	safe := true
	ast.Inspect(scope, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok || c.errors.info.Defs[id] != nil || c.errors.info.ObjectOf(id) != object {
			return true
		}
		for cur := ast.Node(id); cur != nil && cur != scope; cur = c.parents[cur] {
			if call, ok := cur.(*ast.CallExpr); ok {
				if hasErrorType(c.errors.info.TypeOf(call)) {
					safe = false
					return true
				}
			}
			if literal, ok := cur.(*ast.CompositeLit); ok && implementsError(c.errors.info.TypeOf(literal)) {
				safe = false
				return true
			}
			switch parent := c.parents[cur].(type) {
			case *ast.AssignStmt:
				for i, rhs := range parent.Rhs {
					if rhs != cur {
						continue
					}
					for j, left := range parent.Lhs {
						if len(parent.Rhs) != 1 && i != j {
							continue
						}
						if !c.convertedTargetSafe(scope, left, visited) {
							safe = false
						}
					}
				}
				return true
			case *ast.ValueSpec:
				for i, rhs := range parent.Values {
					if rhs != cur {
						continue
					}
					for j, alias := range parent.Names {
						if len(parent.Values) != 1 && i != j {
							continue
						}
						if !c.convertedObjectSafe(scope, c.errors.info.ObjectOf(alias), visited) {
							safe = false
						}
					}
				}
				return true
			case ast.Stmt:
				return true
			}
		}
		return true
	})
	return safe
}

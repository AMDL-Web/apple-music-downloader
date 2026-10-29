package transpile

import (
	"fmt"
	"sort"
	"strings"
)

type kind int

const (
	kU8 kind = iota
	kU32
	kUsize
	kUntyped // an integer literal without a suffix
	kBool
	kArray
	kTuple
	kStruct
	kBytes // &[u8]
	kCtx   // &[u8] named ctx: the fixed-size white-box table
	kPtr
)

type gtype struct {
	k     kind
	name  string // Rust name of a struct or type alias
	elem  *gtype
	n     uint64
	elems []*gtype
}

func (t *gtype) isInt() bool { return t.k == kU8 || t.k == kU32 || t.k == kUsize || t.k == kUntyped }

func (t *gtype) bits() uint64 {
	switch t.k {
	case kU8:
		return 8
	case kU32:
		return 32
	case kUsize:
		return 64
	}
	return 0
}

func (t *gtype) mask() uint64 {
	if t.k == kU8 || t.k == kU32 {
		return 1<<t.bits() - 1
	}
	return ^uint64(0)
}

func sameType(a, b *gtype) bool {
	if a.k != b.k || a.name != b.name || a.n != b.n || len(a.elems) != len(b.elems) {
		return false
	}
	if (a.elem == nil) != (b.elem == nil) || (a.elem != nil && !sameType(a.elem, b.elem)) {
		return false
	}
	for i := range a.elems {
		if !sameType(a.elems[i], b.elems[i]) {
			return false
		}
	}
	return true
}

type value struct {
	code    string
	t       *gtype
	isConst bool
	c       uint64
	op      string // top-level Go binary operator, "" when the code is a primary expression
	unary   bool
}

type variable struct {
	name  string
	t     *gtype
	read  bool
	line  int // index of the declaring line in emitter.lines, -1 for parameters
	param bool
}

type scope struct {
	vars   map[string]*variable
	parent *scope
}

func (s *scope) lookup(name string) *variable {
	for ; s != nil; s = s.parent {
		if v, ok := s.vars[name]; ok {
			return v
		}
	}
	return nil
}

type signature struct {
	params []param
	ret    *typeExpr
}

type emitter struct {
	aliases  map[string]*typeExpr
	structs  map[string]*structItem
	funcs    map[string]signature
	tuples   map[string]*gtype
	reserved map[string]bool
	lines    []string
	declared []*variable
	scope    *scope
}

var nameOverrides = map[string]string{"St": "state", "ST_SIZE": "stSize"}

// goName turns a Rust item name into an unexported Go identifier.
func goName(rust string) string {
	if n, ok := nameOverrides[rust]; ok {
		return n
	}
	parts := strings.Split(rust, "_")
	var b strings.Builder
	for i, p := range parts {
		if p == "" {
			continue
		}
		if i == 0 {
			b.WriteString(strings.ToLower(p[:1]) + p[1:])
			continue
		}
		if strings.ToUpper(p) == p {
			p = strings.ToLower(p)
		}
		b.WriteString(strings.ToUpper(p[:1]) + p[1:])
	}
	return b.String()
}

// fnName keeps Rust function names as they are (round1_mid stays round1_mid):
// they are unexported in Go already, stay greppable against upstream, and
// would collide with the camel-cased struct names (Round1Mid) otherwise.
func fnName(rust string) string { return rust }

var goKeywords = strings.Fields(`break case chan const continue default defer else fallthrough for func go goto if
	import interface map package range return select struct switch type var
	any append bool byte cap clear close complex copy delete error false imag int iota len make max min new nil
	panic print println real recover string true uint uint8 uint32 elem`)

func (e *emitter) typeOf(t *typeExpr, paramName string) (*gtype, error) {
	switch {
	case t.ref && t.slice:
		if t.elem.prim != "u8" {
			return nil, fmt.Errorf("only &[u8] slices are supported")
		}
		if paramName == "ctx" {
			return &gtype{k: kCtx}, nil
		}
		return &gtype{k: kBytes}, nil
	case t.ref:
		cp := *t
		cp.ref, cp.mut = false, false
		inner, err := e.typeOf(&cp, "")
		if err != nil {
			return nil, err
		}
		if inner.k != kArray && inner.k != kStruct {
			return nil, fmt.Errorf("references are supported to arrays and structs only")
		}
		return &gtype{k: kPtr, elem: inner}, nil
	case t.array:
		elem, err := e.typeOf(t.elem, "")
		if err != nil {
			return nil, err
		}
		return &gtype{k: kArray, elem: elem, n: t.n}, nil
	case t.tuple != nil:
		g := &gtype{k: kTuple}
		for _, el := range t.tuple {
			et, err := e.typeOf(el, "")
			if err != nil {
				return nil, err
			}
			g.elems = append(g.elems, et)
		}
		return g, nil
	case t.slice:
		return nil, fmt.Errorf("unsized slice types are not supported")
	}
	switch t.prim {
	case "u8":
		return &gtype{k: kU8}, nil
	case "u32":
		return &gtype{k: kU32}, nil
	case "usize":
		return &gtype{k: kUsize}, nil
	case "bool":
		return &gtype{k: kBool}, nil
	}
	if a, ok := e.aliases[t.prim]; ok {
		g, err := e.typeOf(a, "")
		if err != nil {
			return nil, err
		}
		cp := *g
		cp.name = t.prim
		return &cp, nil
	}
	if _, ok := e.structs[t.prim]; ok {
		return &gtype{k: kStruct, name: t.prim}, nil
	}
	return nil, fmt.Errorf("unknown type %s", t.prim)
}

func (e *emitter) goType(t *gtype) string {
	switch t.k {
	case kU8:
		return "uint8"
	case kU32:
		return "uint32"
	case kUsize:
		return "uint"
	case kBool:
		return "bool"
	case kBytes:
		return "[]byte"
	case kCtx:
		return "*ctxTable"
	case kPtr:
		return "*" + e.goType(t.elem)
	case kStruct:
		return goName(t.name)
	case kArray:
		if t.name != "" {
			return goName(t.name)
		}
		return fmt.Sprintf("[%d]%s", t.n, e.goType(t.elem))
	case kTuple:
		name := "tuple"
		for _, el := range t.elems {
			name += strings.ToUpper(e.goType(el)[:1]) + e.goType(el)[1:]
		}
		e.tuples[name] = t
		return name
	}
	panic(fmt.Sprintf("goType: unexpected kind %d", t.k))
}

func (e *emitter) emit(format string, args ...any) {
	e.lines = append(e.lines, fmt.Sprintf(format, args...))
}

func (e *emitter) push() { e.scope = &scope{vars: map[string]*variable{}, parent: e.scope} }
func (e *emitter) pop()  { e.scope = e.scope.parent }

func (e *emitter) checkLocal(name string) error {
	if e.reserved[name] {
		return fmt.Errorf("local %s collides with a Go keyword, builtin or package-level name", name)
	}
	return nil
}

func constValue(v uint64, t *gtype) value {
	code := fmt.Sprintf("%d", v)
	if v >= 0x100 {
		code = fmt.Sprintf("0x%X", v)
	}
	return value{code: code, t: t, isConst: true, c: v}
}

// typed returns code that keeps v's type when Go would otherwise infer int
// for an untyped constant.
func (e *emitter) typed(v value) (string, error) {
	if !v.isConst {
		return v.code, nil
	}
	if v.t.k == kUntyped {
		return "", fmt.Errorf("untyped integer constant %s needs a type", v.code)
	}
	return e.goType(v.t) + "(" + v.code + ")", nil
}

func wrap(v value, op string, left bool) string {
	if v.op == "" || (left && v.op == op && !isComparison(op)) {
		return v.code
	}
	return "(" + v.code + ")"
}

func unify(x, y value) (*gtype, error) {
	switch {
	case x.t.k == kUntyped:
		return y.t, nil
	case y.t.k == kUntyped:
		return x.t, nil
	case sameType(x.t, y.t):
		return x.t, nil
	}
	return nil, fmt.Errorf("mismatched operand types %s and %s", x.code, y.code)
}

func (e *emitter) binary(op string, x, y value) (value, error) {
	switch op {
	case "+", "-", "*", "&", "|", "^":
		t, err := unify(x, y)
		if err != nil {
			return value{}, err
		}
		if !t.isInt() {
			return value{}, fmt.Errorf("operator %s needs integers", op)
		}
		if x.isConst && y.isConst {
			var r uint64
			switch op {
			case "+":
				r = x.c + y.c
			case "-":
				r = x.c - y.c
			case "*":
				r = x.c * y.c
			case "&":
				r = x.c & y.c
			case "|":
				r = x.c | y.c
			case "^":
				r = x.c ^ y.c
			}
			if t.k == kUntyped && (r > 1<<31 || (op == "-" && y.c > x.c)) {
				return value{}, fmt.Errorf("untyped constant arithmetic out of range")
			}
			return constValue(r&t.mask(), t), nil
		}
		return value{code: wrap(x, op, true) + " " + op + " " + wrap(y, op, false), t: t, op: op}, nil
	case "<<", ">>":
		return e.shift(op, x, y, false)
	case "==", "!=", "<", ">", "<=", ">=":
		t, err := unify(x, y)
		if err != nil {
			return value{}, err
		}
		if !t.isInt() {
			return value{}, fmt.Errorf("comparison needs integers")
		}
		if x.isConst && y.isConst {
			return value{}, fmt.Errorf("constant comparison is not supported")
		}
		return value{code: wrap(x, op, true) + " " + op + " " + wrap(y, op, false), t: &gtype{k: kBool}, op: op}, nil
	case "&&", "||":
		if x.t.k != kBool || y.t.k != kBool {
			return value{}, fmt.Errorf("operator %s needs booleans", op)
		}
		return value{code: wrap(x, op, true) + " " + op + " " + wrap(y, op, false), t: x.t, op: op}, nil
	}
	return value{}, fmt.Errorf("unsupported operator %s", op)
}

// shift applies a shift. Rust's wrapping_shl masks the amount to the type
// width; a plain shift by a constant at or past the width does not compile in
// Rust, so it is rejected here rather than given Go's shift-to-zero meaning.
func (e *emitter) shift(op string, x, y value, wrapping bool) (value, error) {
	if !x.t.isInt() || !y.t.isInt() {
		return value{}, fmt.Errorf("shift needs integers")
	}
	if x.t.k == kUntyped {
		return value{}, fmt.Errorf("shift of an untyped constant")
	}
	w := x.t.bits()
	if !y.isConst {
		if !wrapping {
			return value{}, fmt.Errorf("shift by a non-constant amount")
		}
		y = value{code: fmt.Sprintf("%s & %d", wrap(y, "&", true), w-1), t: y.t, op: "&"}
	} else {
		if wrapping {
			y = constValue(y.c&(w-1), y.t)
		} else if y.c >= w {
			return value{}, fmt.Errorf("shift amount %d overflows a %d-bit operand", y.c, w)
		}
		if x.isConst {
			r := x.c >> y.c
			if op == "<<" {
				r = x.c << y.c
			}
			return constValue(r&x.t.mask(), x.t), nil
		}
	}
	return value{code: wrap(x, op, true) + " " + op + " " + wrap(y, op, false), t: x.t, op: op}, nil
}

func (e *emitter) unary(op string, x value) (value, error) {
	code := x.code
	if x.op != "" || x.unary {
		code = "(" + code + ")"
	}
	switch {
	case op == "!" && x.t.k == kBool:
		return value{code: "!" + code, t: x.t, unary: true}, nil
	case op == "!" && x.t.isInt() && x.t.k != kUntyped:
		if x.isConst {
			return constValue(^x.c&x.t.mask(), x.t), nil
		}
		return value{code: "^" + code, t: x.t, unary: true}, nil
	case op == "-" && x.t.isInt() && x.t.k != kUntyped:
		if x.isConst {
			return constValue(-x.c&x.t.mask(), x.t), nil
		}
		return value{code: "-" + code, t: x.t, unary: true}, nil
	}
	return value{}, fmt.Errorf("unsupported unary %s on %s", op, x.code)
}

func (e *emitter) expr(x expr) (value, error) {
	switch x := x.(type) {
	case *intLit:
		t := &gtype{k: kUntyped}
		switch x.suffix {
		case "u8":
			t = &gtype{k: kU8}
		case "u32":
			t = &gtype{k: kU32}
		case "usize":
			t = &gtype{k: kUsize}
		}
		if t.k != kUntyped && x.val > t.mask() {
			return value{}, fmt.Errorf("literal %s overflows its type", x.text)
		}
		return value{code: x.text, t: t, isConst: true, c: x.val}, nil
	case *identExpr:
		v := e.scope.lookup(x.name)
		if v == nil {
			return value{}, fmt.Errorf("undefined: %s", x.name)
		}
		v.read = true
		return value{code: x.name, t: v.t}, nil
	case *fieldExpr:
		base, err := e.expr(x.x)
		if err != nil {
			return value{}, err
		}
		st := base.t
		if st.k == kPtr {
			st = st.elem
		}
		if st.k != kStruct {
			return value{}, fmt.Errorf("field %s on a non-struct", x.name)
		}
		for _, f := range e.structs[st.name].fields {
			if f.name == x.name {
				ft, err := e.typeOf(f.typ, "")
				if err != nil {
					return value{}, err
				}
				return value{code: wrap(base, ".", true) + "." + x.name, t: ft}, nil
			}
		}
		return value{}, fmt.Errorf("struct %s has no field %s", st.name, x.name)
	case *indexExpr:
		base, err := e.expr(x.x)
		if err != nil {
			return value{}, err
		}
		idx, err := e.expr(x.idx)
		if err != nil {
			return value{}, err
		}
		if !idx.t.isInt() {
			return value{}, fmt.Errorf("non-integer index %s", idx.code)
		}
		var elem *gtype
		var n uint64
		switch base.t.k {
		case kBytes:
			elem = &gtype{k: kU8}
		case kPtr:
			if base.t.elem.k != kArray {
				return value{}, fmt.Errorf("index of a non-array pointer")
			}
			elem, n = base.t.elem.elem, base.t.elem.n
		case kArray:
			elem, n = base.t.elem, base.t.n
		default:
			return value{}, fmt.Errorf("index of %s", base.code)
		}
		if n > 0 && idx.isConst && idx.c >= n {
			return value{}, fmt.Errorf("constant index %d out of range [0,%d)", idx.c, n)
		}
		return value{code: wrap(base, "[", true) + "[" + idx.code + "]", t: elem}, nil
	case *castExpr:
		v, err := e.expr(x.x)
		if err != nil {
			return value{}, err
		}
		var to *gtype
		switch x.to {
		case "u8":
			to = &gtype{k: kU8}
		case "u32":
			to = &gtype{k: kU32}
		case "usize":
			to = &gtype{k: kUsize}
		default:
			return value{}, fmt.Errorf("unsupported cast to %s", x.to)
		}
		if !v.t.isInt() {
			return value{}, fmt.Errorf("cast of a non-integer")
		}
		if v.isConst {
			if v.c&to.mask() == v.c {
				// Keep the literal as written so st[616] greps against upstream.
				return value{code: v.code, t: to, isConst: true, c: v.c}, nil
			}
			return constValue(v.c&to.mask(), to), nil
		}
		if v.t.k == to.k {
			return v, nil
		}
		return value{code: e.goType(to) + "(" + v.code + ")", t: to}, nil
	case *unaryExpr:
		v, err := e.expr(x.x)
		if err != nil {
			return value{}, err
		}
		return e.unary(x.op, v)
	case *binaryExpr:
		l, err := e.expr(x.x)
		if err != nil {
			return value{}, err
		}
		r, err := e.expr(x.y)
		if err != nil {
			return value{}, err
		}
		return e.binary(x.op, l, r)
	case *methodExpr:
		recv, err := e.expr(x.recv)
		if err != nil {
			return value{}, err
		}
		if !recv.t.isInt() {
			return value{}, fmt.Errorf("method %s on a non-integer", x.method)
		}
		var args []value
		for _, a := range x.args {
			v, err := e.expr(a)
			if err != nil {
				return value{}, err
			}
			args = append(args, v)
		}
		switch {
		case x.method == "wrapping_add" && len(args) == 1:
			return e.binary("+", recv, args[0])
		case x.method == "wrapping_sub" && len(args) == 1:
			return e.binary("-", recv, args[0])
		case x.method == "wrapping_mul" && len(args) == 1:
			return e.binary("*", recv, args[0])
		case x.method == "wrapping_neg" && len(args) == 0:
			return e.unary("-", recv)
		case x.method == "wrapping_shl" && len(args) == 1:
			return e.shift("<<", recv, args[0], true)
		}
		return value{}, fmt.Errorf("unsupported method %s", x.method)
	case *callExpr:
		sig, ok := e.funcs[x.fn]
		if !ok {
			return value{}, fmt.Errorf("call to unknown function %s", x.fn)
		}
		if len(x.args) != len(sig.params) {
			return value{}, fmt.Errorf("%s: want %d arguments, got %d", x.fn, len(sig.params), len(x.args))
		}
		var codes []string
		for i, a := range x.args {
			v, err := e.expr(a)
			if err != nil {
				return value{}, err
			}
			pt, err := e.typeOf(sig.params[i].typ, sig.params[i].name)
			if err != nil {
				return value{}, err
			}
			if !assignable(v, pt) {
				return value{}, fmt.Errorf("%s: argument %d has the wrong type", x.fn, i+1)
			}
			codes = append(codes, v.code)
		}
		if sig.ret == nil {
			return value{}, fmt.Errorf("%s returns nothing", x.fn)
		}
		rt, err := e.typeOf(sig.ret, "")
		if err != nil {
			return value{}, err
		}
		return value{code: fnName(x.fn) + "(" + strings.Join(codes, ", ") + ")", t: rt}, nil
	case *structExpr:
		si, ok := e.structs[x.name]
		if !ok || len(x.fields) != len(si.fields) {
			return value{}, fmt.Errorf("struct literal %s does not match its declaration", x.name)
		}
		seen := map[string]bool{}
		var parts []string
		for _, f := range x.fields {
			var decl *fieldDecl
			for i := range si.fields {
				if si.fields[i].name == f.name {
					decl = &si.fields[i]
				}
			}
			if decl == nil || seen[f.name] {
				return value{}, fmt.Errorf("struct literal %s: unknown or repeated field %s", x.name, f.name)
			}
			seen[f.name] = true
			v, err := e.expr(f.val)
			if err != nil {
				return value{}, err
			}
			ft, err := e.typeOf(decl.typ, "")
			if err != nil {
				return value{}, err
			}
			if !assignable(v, ft) {
				return value{}, fmt.Errorf("struct literal %s: field %s has the wrong type", x.name, f.name)
			}
			parts = append(parts, f.name+": "+v.code)
		}
		return value{code: goName(x.name) + "{" + strings.Join(parts, ", ") + "}", t: &gtype{k: kStruct, name: x.name}}, nil
	case *arrayExpr:
		return e.array(x)
	case *repeatExpr:
		v, err := e.expr(x.elem)
		if err != nil {
			return value{}, err
		}
		if !v.isConst || v.c != 0 || v.t.k == kUntyped {
			return value{}, fmt.Errorf("only typed zero array repeats are supported")
		}
		t := &gtype{k: kArray, elem: v.t, n: x.n}
		return value{code: e.goType(t) + "{}", t: t}, nil
	case *tupleExpr:
		return value{}, fmt.Errorf("tuple expression outside an array or let")
	}
	return value{}, fmt.Errorf("unsupported expression %T", x)
}

func assignable(v value, t *gtype) bool {
	if v.t.k == kUntyped {
		return t.isInt() && t.k != kUntyped && v.c <= t.mask()
	}
	return sameType(v.t, t)
}

// array emits an array literal. Tuple elements become anonymous struct
// literals of a generated tupleXxx type.
func (e *emitter) array(x *arrayExpr) (value, error) {
	var elemType *gtype
	var codes []string
	for _, el := range x.elems {
		var v value
		var err error
		if tup, ok := el.(*tupleExpr); ok {
			v, err = e.tuple(tup)
		} else {
			v, err = e.expr(el)
		}
		if err != nil {
			return value{}, err
		}
		if v.t.k == kUntyped {
			return value{}, fmt.Errorf("untyped array element %s", v.code)
		}
		if elemType == nil {
			elemType = v.t
		} else if !sameType(elemType, v.t) {
			return value{}, fmt.Errorf("array elements of different types")
		}
		codes = append(codes, v.code)
	}
	t := &gtype{k: kArray, elem: elemType, n: uint64(len(x.elems))}
	return value{code: e.goType(t) + "{" + strings.Join(codes, ", ") + "}", t: t}, nil
}

// tuple emits the brace-elided element of an array of tuples.
func (e *emitter) tuple(x *tupleExpr) (value, error) {
	t := &gtype{k: kTuple}
	var codes []string
	for _, el := range x.elems {
		v, err := e.expr(el)
		if err != nil {
			return value{}, err
		}
		if v.t.k == kUntyped || !v.t.isInt() {
			return value{}, fmt.Errorf("tuple elements must be typed integers")
		}
		t.elems = append(t.elems, v.t)
		codes = append(codes, v.code)
	}
	e.goType(t)
	return value{code: "{" + strings.Join(codes, ", ") + "}", t: t}, nil
}

func (e *emitter) declare(name string, v value) error {
	if err := e.checkLocal(name); err != nil {
		return err
	}
	if prev, ok := e.scope.vars[name]; ok {
		// Rust shadowing within one scope: the old binding is unreachable from
		// here on, so reassigning the same Go variable is equivalent.
		if !sameType(prev.t, v.t) && !assignable(v, prev.t) {
			return fmt.Errorf("let %s changes the binding's type", name)
		}
		if v.code == name {
			// let mut x = x; only makes a parameter mutable, which Go
			// parameters already are.
			return nil
		}
		e.emit("%s = %s", name, v.code)
		return nil
	}
	if v.t.k == kTuple {
		return fmt.Errorf("let %s binds a tuple", name)
	}
	code, err := e.typed(v)
	if err != nil {
		return err
	}
	nv := &variable{name: name, t: v.t, line: len(e.lines)}
	e.scope.vars[name] = nv
	e.declared = append(e.declared, nv)
	e.emit("%s := %s", name, code)
	return nil
}

func (e *emitter) stmts(list []stmt) error {
	for _, s := range list {
		if err := e.stmt(s); err != nil {
			return err
		}
	}
	return nil
}

func (e *emitter) stmt(s stmt) error {
	switch s := s.(type) {
	case *commentStmt:
		e.emit("//%s", s.text)
	case *letStmt:
		if !s.tuple {
			v, err := e.expr(s.init)
			if err != nil {
				return err
			}
			return e.declare(s.names[0], v)
		}
		tup, ok := s.init.(*tupleExpr)
		if !ok || len(tup.elems) != len(s.names) {
			return fmt.Errorf("tuple let needs a tuple literal of the same arity")
		}
		var vals []value
		for _, el := range tup.elems {
			v, err := e.expr(el)
			if err != nil {
				return err
			}
			if !v.isConst {
				return fmt.Errorf("tuple let supports constant elements only")
			}
			vals = append(vals, v)
		}
		for i, n := range s.names {
			if err := e.declare(n, vals[i]); err != nil {
				return err
			}
		}
	case *assignStmt:
		return e.assign(s)
	case *ifStmt:
		cond, err := e.expr(s.cond)
		if err != nil {
			return err
		}
		if cond.t.k != kBool {
			return fmt.Errorf("if condition is not a bool")
		}
		e.emit("if %s {", cond.code)
		e.push()
		if err := e.stmts(s.then); err != nil {
			return err
		}
		e.pop()
		if s.els != nil {
			e.emit("} else {")
			e.push()
			if err := e.stmts(s.els); err != nil {
				return err
			}
			e.pop()
		}
		e.emit("}")
	case *forStmt:
		m, ok := s.iter.(*methodExpr)
		if !ok || m.method != "iter" || len(m.args) != 0 {
			return fmt.Errorf("for loops must iterate an array literal with .iter()")
		}
		arr, ok := m.recv.(*arrayExpr)
		if !ok {
			return fmt.Errorf("for loops must iterate an array literal with .iter()")
		}
		av, err := e.array(arr)
		if err != nil {
			return err
		}
		if av.t.elem.k != kTuple || len(av.t.elem.elems) != len(s.names) {
			return fmt.Errorf("for pattern does not match the tuple element type")
		}
		e.emit("for _, elem := range %s {", av.code)
		e.push()
		for i, n := range s.names {
			if err := e.declare(n, value{code: fmt.Sprintf("elem.f%d", i), t: av.t.elem.elems[i]}); err != nil {
				return err
			}
		}
		if err := e.stmts(s.body); err != nil {
			return err
		}
		e.pop()
		e.emit("}")
	case *returnStmt:
		v, err := e.expr(s.x)
		if err != nil {
			return err
		}
		e.emit("return %s", v.code)
	default:
		return fmt.Errorf("unsupported statement %T", s)
	}
	return nil
}

func (e *emitter) assign(s *assignStmt) error {
	var lhs string
	var lt *gtype
	switch l := s.lhs.(type) {
	case *identExpr:
		v := e.scope.lookup(l.name)
		if v == nil {
			return fmt.Errorf("assignment to undefined %s", l.name)
		}
		lhs, lt = l.name, v.t
	case *indexExpr:
		lv, err := e.expr(l)
		if err != nil {
			return err
		}
		lhs, lt = lv.code, lv.t
	default:
		return fmt.Errorf("unsupported assignment target")
	}
	rhs, err := e.expr(s.rhs)
	if err != nil {
		return err
	}
	switch s.op {
	case "=":
		if !assignable(rhs, lt) {
			return fmt.Errorf("assignment to %s has the wrong type", lhs)
		}
		if rhs.code == lhs {
			return fmt.Errorf("self-assignment of %s", lhs)
		}
	case "^=", "&=", "|=", "+=", "-=", "*=":
		if !lt.isInt() || !assignable(rhs, lt) {
			return fmt.Errorf("compound assignment to %s has the wrong type", lhs)
		}
	default:
		return fmt.Errorf("unsupported assignment operator %s", s.op)
	}
	e.emit("%s %s %s", lhs, s.op, rhs.code)
	return nil
}

func (e *emitter) fn(f *fnItem) error {
	e.lines = nil
	e.declared = nil
	e.scope = &scope{vars: map[string]*variable{}}
	var params []string
	for _, p := range f.params {
		if err := e.checkLocal(p.name); err != nil {
			return err
		}
		t, err := e.typeOf(p.typ, p.name)
		if err != nil {
			return fmt.Errorf("parameter %s: %w", p.name, err)
		}
		e.scope.vars[p.name] = &variable{name: p.name, t: t, line: -1, param: true}
		params = append(params, p.name+" "+e.goType(t))
	}
	ret := ""
	if f.ret != nil {
		rt, err := e.typeOf(f.ret, "")
		if err != nil {
			return err
		}
		ret = " " + e.goType(rt)
	}
	header := fmt.Sprintf("func %s(%s)%s {", fnName(f.name), strings.Join(params, ", "), ret)
	if err := e.stmts(f.body); err != nil {
		return fmt.Errorf("fn %s: %w", f.name, err)
	}
	// Go rejects locals that are never read; Rust only warns.
	sort.SliceStable(e.declared, func(i, j int) bool { return e.declared[i].line > e.declared[j].line })
	for _, v := range e.declared {
		if !v.read {
			i := v.line + 1
			e.lines = append(e.lines[:i], append([]string{"_ = " + v.name}, e.lines[i:]...)...)
		}
	}
	e.lines = append([]string{header}, append(e.lines, "}")...)
	return nil
}

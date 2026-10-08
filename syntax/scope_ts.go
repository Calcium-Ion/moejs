package syntax

import "slices"

// TypeScript modules: the parser has erased the type syntax (typescript.go)
// and recorded the names declared only as types (Module.ts); the scope pass
// drops what refers to them, as tsc does when it emits JavaScript.

// declareAliases declares the aliases of import x = A.B in the module scope
// s, so that a use of x as a value resolves to them (and fails in
// elideTypeImports) and a declaration of the same name clashes with them.
func (r *resolver) declareAliases(s *Scope) {
	for _, id := range r.module.ts.aliases {
		b := r.declare(s, id, BindConst, 0)
		id.Binding = b
		info := r.module.ts
		if info.aliasBindings == nil {
			info.aliasBindings = make(map[*Binding]*Ident)
		}
		info.aliasBindings[b] = id
	}
}

// tsTypeExport reports whether the local export name, which the module
// does not declare as a value, names a type, so that the export is
// dropped (export { T }).
func (r *resolver) tsTypeExport(name string) bool {
	return r.module.ts != nil && r.module.ts.names[name] != 0
}

// tsTypeDefault reports whether export default x exports a type, which
// drops it: x names a type and nothing else. A name declare declares is a
// value without a run-time binding, so the export reads the global of that
// name, as tsc and esbuild emit it.
func (r *resolver) tsTypeDefault(x Node) bool {
	id, ok := x.(*Ident)
	if !ok || r.module.ts == nil || r.scope.Resolve(id.Name) != nil {
		return false
	}
	return r.module.ts.names[id.Name] == tsTypeName
}

// tsDrop marks an export specifier or declaration for elideTypeImports to
// drop.
func (r *resolver) tsDropNode(n Node) {
	info := r.module.ts
	if info.drop == nil {
		info.drop = make(map[Node]bool)
	}
	info.drop[n] = true
}

// elideTypeImports finishes the scope pass of a TypeScript module. An
// import binding that no expression reads is dropped, as tsc drops an
// import used only as a type, and so is an import declaration left without
// bindings (import "m" stays); so are the exports of types the pass marked
// (tsDropNode). A use of an import alias as a value fails: tsc emits a
// variable for it. The module scope, the import entries and the module
// requests are rebuilt without what was dropped.
func (r *resolver) elideTypeImports() {
	m := r.module
	info := m.ts
	used := make(map[*Binding]bool)
	for _, s := range m.Body {
		if _, ok := s.(*ImportDecl); ok || info.drop[s] {
			continue
		}
		Inspect(s, func(n Node) bool {
			if id, ok := n.(*Ident); ok && id.Binding != nil {
				if b := id.Binding; b.Kind == BindImport || b.Kind == BindImportNS || info.aliasBindings[b] != nil {
					used[b] = true
				}
			}
			return true
		})
	}
	drop := make(map[*Binding]bool)
	for _, id := range m.ts.aliases {
		if b := id.Binding; used[b] {
			r.fail(id.Pos, "An import alias used as a value is not supported: only erasable TypeScript is (see TODO.md)")
		}
	}
	for b := range info.aliasBindings {
		drop[b] = true
	}
	for _, ie := range m.Imports {
		if !used[ie.Binding] {
			drop[ie.Binding] = true
		}
	}

	body := m.Body[:0]
	for _, s := range m.Body {
		switch s := s.(type) {
		case *ImportDecl:
			bound := s.Default != nil || s.Namespace != nil || len(s.Specs) > 0
			if s.Default != nil && drop[s.Default.Binding] {
				s.Default = nil
			}
			if s.Namespace != nil && drop[s.Namespace.Binding] {
				s.Namespace = nil
			}
			s.Specs = slices.DeleteFunc(s.Specs, func(spec *ImportSpec) bool { return drop[spec.Local.Binding] })
			if len(s.Specs) == 0 {
				s.Specs = nil // import D from "m", not import D {} from "m"
			}
			if bound && s.Default == nil && s.Namespace == nil && s.Specs == nil {
				continue
			}
		case *ExportNamed:
			if s.Source == nil {
				s.Specs = slices.DeleteFunc(s.Specs, func(spec *ExportSpec) bool { return info.drop[spec] })
				if len(s.Specs) == 0 {
					continue
				}
			}
		case *ExportDefault:
			if info.drop[s] {
				drop[m.Scope.Lookup("*default*")] = true
				continue
			}
		}
		body = append(body, s)
	}
	clear(m.Body[len(body):])
	m.Body = body
	if len(drop) == 0 {
		return
	}

	root := m.Scope
	kept := root.Bindings[:0]
	for _, b := range root.Bindings {
		if drop[b] {
			continue
		}
		b.Slot = len(kept)
		kept = append(kept, b)
	}
	clear(root.Bindings[len(kept):])
	root.Bindings = kept
	if root.names != nil {
		root.names = make(map[string]*Binding, len(kept))
		for _, b := range kept {
			root.names[b.Name] = b
		}
	}
	m.Imports = slices.DeleteFunc(m.Imports, func(ie *ImportEntry) bool { return drop[ie.Binding] })

	// The requests left, in the order of the statements that make them.
	var reqs []*ModuleRequest
	request := func(src *StringLit) {
		for _, req := range reqs {
			if req.Specifier == src.Value {
				return
			}
		}
		reqs = append(reqs, &ModuleRequest{Specifier: src.Value, Pos: src.Pos})
	}
	for _, s := range m.Body {
		switch s := s.(type) {
		case *ImportDecl:
			request(s.Source)
		case *ExportNamed:
			if s.Source != nil {
				request(s.Source)
			}
		case *ExportAll:
			request(s.Source)
		}
	}
	index := func(i int) int {
		return slices.IndexFunc(reqs, func(req *ModuleRequest) bool { return req.Specifier == m.Requests[i].Specifier })
	}
	for _, ie := range m.Imports {
		ie.Request = index(ie.Request)
	}
	for _, re := range m.Reexports {
		re.Request = index(re.Request)
	}
	for _, st := range m.Stars {
		st.Request = index(st.Request)
	}
	m.Requests = reqs
}

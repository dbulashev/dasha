package sqlparse

import (
	pgast "github.com/pganalyze/pg_query_go/v6"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// describe reduces a parse tree to the facts an index advisor uses. Everything
// here is structural: no catalog is consulted, so an unqualified name stays
// unqualified and an unattributable column stays unattributed.
func describe(tree *pgast.ParseResult) Statement {
	b := newBuilder()

	for i, raw := range tree.GetStmts() {
		if i == 0 {
			b.stmt.Kind = kindOf(raw.GetStmt())
		}

		b.walkStmt(raw.GetStmt(), nil)
	}

	return b.stmt
}

func kindOf(n *pgast.Node) Kind {
	switch n.GetNode().(type) {
	case nil:
		return KindOther
	case *pgast.Node_SelectStmt:
		return KindSelect
	case *pgast.Node_InsertStmt:
		return KindInsert
	case *pgast.Node_UpdateStmt:
		return KindUpdate
	case *pgast.Node_DeleteStmt:
		return KindDelete
	case *pgast.Node_MergeStmt:
		return KindMerge
	default:
		return KindUtility
	}
}

type refKey struct {
	schema string
	name   string
}

type colRef struct {
	qualifier string
	name      string
}

type builder struct {
	stmt        Statement
	seenTable   map[refKey]struct{}
	seenWritten map[refKey]struct{}
	seenUsage   map[Usage]struct{}
	seenReason  map[string]struct{}
}

func newBuilder() *builder {
	return &builder{
		seenTable:   make(map[refKey]struct{}),
		seenWritten: make(map[refKey]struct{}),
		seenUsage:   make(map[Usage]struct{}),
		seenReason:  make(map[string]struct{}),
	}
}

// addTable keys on schema and name, not on the alias: a self-join names the same
// relation twice, and a caller resolving names against the catalog wants it once.
func (b *builder) addTable(r Ref) {
	k := refKey{r.Schema, r.Name}
	if _, ok := b.seenTable[k]; ok {
		return
	}

	b.seenTable[k] = struct{}{}
	b.stmt.Tables = append(b.stmt.Tables, r)
}

func (b *builder) addWritten(rv *pgast.RangeVar) (Ref, bool) {
	if rv == nil {
		return Ref{}, false
	}

	ref := refOf(rv)
	b.addTable(ref)

	k := refKey{ref.Schema, ref.Name}
	if _, ok := b.seenWritten[k]; !ok {
		b.seenWritten[k] = struct{}{}
		b.stmt.Written = append(b.stmt.Written, ref)
	}

	return ref, true
}

func (b *builder) note(code string) {
	if _, ok := b.seenReason[code]; ok {
		return
	}

	b.seenReason[code] = struct{}{}
	b.stmt.Unsupported = append(b.stmt.Unsupported, code)
}

// scope is what a column name can refer to at one nesting level. Subqueries and
// CTE bodies get their own; lookups walk outwards, which is how a correlated
// subquery reaches the outer table.
type scope struct {
	parent *scope
	tables map[string]Ref
	ctes   map[string]struct{}
	items  int
	only   *Ref // the single FROM item, when it is exactly one and it is a table
}

func newScope(parent *scope) *scope {
	return &scope{
		parent: parent,
		tables: make(map[string]Ref),
		ctes:   make(map[string]struct{}),
	}
}

func (s *scope) addTable(r Ref) {
	name := r.Alias
	if name == "" {
		name = r.Name
	}

	if _, exists := s.tables[name]; !exists {
		s.tables[name] = r
	}

	s.items++

	if s.items == 1 {
		ref := r
		s.only = &ref

		return
	}

	s.only = nil
}

// addOpaque records a FROM item that resolves to nothing indexable — a CTE
// reference, a subquery, a function scan. It still occupies a slot, so bare
// columns in this scope can no longer be attributed to a single table.
func (s *scope) addOpaque() {
	s.items++
	s.only = nil
}

func (s *scope) addCTE(name string) { s.ctes[name] = struct{}{} }

func (s *scope) isCTE(name string) bool {
	for cur := s; cur != nil; cur = cur.parent {
		if _, ok := cur.ctes[name]; ok {
			return true
		}
	}

	return false
}

func (s *scope) resolve(qualifier string) (Ref, bool) {
	for cur := s; cur != nil; cur = cur.parent {
		if r, ok := cur.tables[qualifier]; ok {
			return r, true
		}
	}

	return Ref{}, false
}

// sole returns the table a bare column must belong to, if there is only one.
func (s *scope) sole() (Ref, bool) {
	for cur := s; cur != nil; cur = cur.parent {
		if cur.items == 0 {
			continue
		}

		if cur.only == nil {
			return Ref{}, false
		}

		return *cur.only, true
	}

	return Ref{}, false
}

func (b *builder) walkStmt(n *pgast.Node, sc *scope) {
	switch s := n.GetNode().(type) {
	case *pgast.Node_SelectStmt:
		b.walkSelect(s.SelectStmt, sc)
	case *pgast.Node_InsertStmt:
		b.walkInsert(s.InsertStmt, sc)
	case *pgast.Node_UpdateStmt:
		b.walkUpdate(s.UpdateStmt, sc)
	case *pgast.Node_DeleteStmt:
		b.walkDelete(s.DeleteStmt, sc)
	case *pgast.Node_MergeStmt:
		b.walkMerge(s.MergeStmt, sc)
	}
}

func (b *builder) walkSelect(s *pgast.SelectStmt, parent *scope) {
	if s == nil {
		return
	}

	sc := newScope(parent)
	b.walkWith(s.GetWithClause(), sc)

	// A set operation has no FROM of its own, and its ORDER BY addresses output
	// columns rather than table columns — each arm is analyzed on its own.
	if s.GetLarg() != nil || s.GetRarg() != nil {
		b.walkSelect(s.GetLarg(), sc)
		b.walkSelect(s.GetRarg(), sc)

		return
	}

	for _, item := range s.GetFromClause() {
		b.walkFrom(item, sc)
	}

	b.walkQual(s.GetWhereClause(), sc)
	b.walkSort(s.GetSortClause(), sc)
	b.walkGroup(s.GetGroupClause(), sc)

	for _, target := range s.GetTargetList() {
		b.walkSubLinks(target, sc)
	}

	b.walkSubLinks(s.GetHavingClause(), sc)
}

func (b *builder) walkInsert(s *pgast.InsertStmt, parent *scope) {
	if s == nil {
		return
	}

	sc := newScope(parent)
	b.walkWith(s.GetWithClause(), sc)
	b.addWritten(s.GetRelation()) // the target is not in scope for the source query
	b.walkStmt(s.GetSelectStmt(), sc)
}

func (b *builder) walkUpdate(s *pgast.UpdateStmt, parent *scope) {
	if s == nil {
		return
	}

	sc := newScope(parent)
	b.walkWith(s.GetWithClause(), sc)

	if ref, ok := b.addWritten(s.GetRelation()); ok {
		sc.addTable(ref)
	}

	for _, item := range s.GetFromClause() {
		b.walkFrom(item, sc)
	}

	b.walkQual(s.GetWhereClause(), sc)

	for _, target := range s.GetTargetList() {
		b.walkSubLinks(target, sc)
	}
}

func (b *builder) walkDelete(s *pgast.DeleteStmt, parent *scope) {
	if s == nil {
		return
	}

	sc := newScope(parent)
	b.walkWith(s.GetWithClause(), sc)

	if ref, ok := b.addWritten(s.GetRelation()); ok {
		sc.addTable(ref)
	}

	for _, item := range s.GetUsingClause() {
		b.walkFrom(item, sc)
	}

	b.walkQual(s.GetWhereClause(), sc)
}

func (b *builder) walkMerge(s *pgast.MergeStmt, parent *scope) {
	if s == nil {
		return
	}

	sc := newScope(parent)
	b.walkWith(s.GetWithClause(), sc)

	if ref, ok := b.addWritten(s.GetRelation()); ok {
		sc.addTable(ref)
	}

	b.walkFrom(s.GetSourceRelation(), sc)
	b.walkQual(s.GetJoinCondition(), sc)

	for _, when := range s.GetMergeWhenClauses() {
		b.walkSubLinks(when, sc)
	}
}

// walkWith registers every CTE name before walking any body, so that a recursive
// CTE referring to itself is not mistaken for a table.
func (b *builder) walkWith(w *pgast.WithClause, sc *scope) {
	if w == nil {
		return
	}

	for _, item := range w.GetCtes() {
		if cte := item.GetCommonTableExpr(); cte != nil {
			sc.addCTE(cte.GetCtename())
		}
	}

	for _, item := range w.GetCtes() {
		if cte := item.GetCommonTableExpr(); cte != nil {
			b.walkStmt(cte.GetCtequery(), sc)
		}
	}
}

func (b *builder) walkFrom(n *pgast.Node, sc *scope) {
	if n == nil {
		return
	}

	switch f := n.GetNode().(type) {
	case *pgast.Node_RangeVar:
		b.addFromTable(f.RangeVar, sc)
	case *pgast.Node_JoinExpr:
		b.walkJoin(f.JoinExpr, sc)
	case *pgast.Node_RangeSubselect:
		sc.addOpaque()
		b.walkStmt(f.RangeSubselect.GetSubquery(), sc)
	default:
		// Function scans, VALUES, TABLESAMPLE — nothing to index, but the slot is
		// taken and bare columns are no longer unambiguous.
		sc.addOpaque()
		b.walkSubLinks(n, sc)
	}
}

func (b *builder) addFromTable(rv *pgast.RangeVar, sc *scope) {
	if rv == nil {
		return
	}

	// A WITH name is written exactly like a table in FROM and is not one.
	if rv.GetSchemaname() == "" && sc.isCTE(rv.GetRelname()) {
		sc.addOpaque()

		return
	}

	ref := refOf(rv)
	b.addTable(ref)
	sc.addTable(ref)
}

func (b *builder) walkJoin(j *pgast.JoinExpr, sc *scope) {
	if j == nil {
		return
	}

	b.walkFrom(j.GetLarg(), sc)
	b.walkFrom(j.GetRarg(), sc)
	b.walkQual(j.GetQuals(), sc)

	if len(j.GetUsingClause()) == 0 {
		return
	}

	// USING (a) names the column on both sides at once and writes no qualifier,
	// so each side is attributed from its own FROM item.
	left, lok := tableOfItem(j.GetLarg(), sc)
	right, rok := tableOfItem(j.GetRarg(), sc)

	for _, item := range j.GetUsingClause() {
		column := item.GetString_().GetSval()
		if column == "" {
			continue
		}

		if lok {
			b.addUsageRef(left, column, RoleJoin, false, 0)
		}

		if rok {
			b.addUsageRef(right, column, RoleJoin, false, 0)
		}
	}
}

func (b *builder) walkQual(n *pgast.Node, sc *scope) {
	if n == nil {
		return
	}

	switch q := n.GetNode().(type) {
	case *pgast.Node_BoolExpr:
		b.walkBool(q.BoolExpr, sc)
	case *pgast.Node_AExpr:
		b.walkAExpr(q.AExpr, sc)
	case *pgast.Node_NullTest:
		b.walkNullTest(q.NullTest, sc)
	case *pgast.Node_SubLink:
		b.walkSubLink(q.SubLink, sc, true)
	default:
		// CASE, a boolean function call, a row comparison: mentions a column, but
		// no plain btree index on that column can serve it.
		if hasColumnRef(n) {
			b.note(ReasonExpressionPredicate)
		}

		b.walkSubLinks(n, sc)
	}
}

func (b *builder) walkBool(e *pgast.BoolExpr, sc *scope) {
	if e == nil {
		return
	}

	if e.GetBoolop() == pgast.BoolExprType_AND_EXPR {
		for _, arg := range e.GetArgs() {
			b.walkQual(arg, sc)
		}

		return
	}

	// OR and NOT: a column from one branch only helps when every branch is
	// indexed, which step 1 does not evaluate. Subqueries inside the branches are
	// still analyzed — their predicates are independent of the branching.
	if e.GetBoolop() == pgast.BoolExprType_OR_EXPR {
		b.note(ReasonOrPredicate)
	}

	for _, arg := range e.GetArgs() {
		b.walkSubLinks(arg, sc)
	}
}

func (b *builder) walkAExpr(e *pgast.A_Expr, sc *scope) {
	if e == nil {
		return
	}

	op := lastName(e.GetName())
	left, lok := columnOf(e.GetLexpr())
	right, rok := columnOf(e.GetRexpr())

	switch e.GetKind() {
	case pgast.A_Expr_Kind_AEXPR_OP:
		switch {
		case isEqualOp(op) && lok && rok:
			b.addUsage(sc, left, RoleJoin, false, 0)
			b.addUsage(sc, right, RoleJoin, false, 0)
		case isEqualOp(op) && lok:
			b.addUsage(sc, left, RoleEquality, false, 0)
		case isEqualOp(op) && rok:
			b.addUsage(sc, right, RoleEquality, false, 0)
		case isRangeOp(op) && lok != rok:
			column := left
			if rok {
				column = right
			}

			b.addUsage(sc, column, RoleRange, false, 0)
		case isEqualOp(op) || isRangeOp(op):
			b.noteWrappedColumn(e.GetLexpr(), e.GetRexpr())
		}
	case pgast.A_Expr_Kind_AEXPR_OP_ANY, pgast.A_Expr_Kind_AEXPR_IN:
		// The same shape twice: pg_stat_statements normalizes IN (...) to = ANY($1).
		if isEqualOp(op) && lok {
			b.addUsage(sc, left, RoleEquality, false, 0)
		}
	case pgast.A_Expr_Kind_AEXPR_BETWEEN, pgast.A_Expr_Kind_AEXPR_BETWEEN_SYM:
		if lok {
			b.addUsage(sc, left, RoleRange, false, 0)
		}
	}

	b.walkSubLinks(e.GetRexpr(), sc)
}

func (b *builder) walkNullTest(t *pgast.NullTest, sc *scope) {
	// IS NOT NULL is left out on purpose: it matches nearly the whole table on the
	// columns people write it against, so it is not an equality in any useful sense.
	if t == nil || t.GetNulltesttype() != pgast.NullTestType_IS_NULL {
		return
	}

	if column, ok := columnOf(t.GetArg()); ok {
		b.addUsage(sc, column, RoleEquality, false, 0)
	}
}

// walkSubLink analyzes a subquery. predicate says whether the subquery itself
// stands in an AND-connected position: only then does its outer column count as
// a filter. Reached from under an OR or a NOT, the subquery's own WHERE is still
// worth reading, but the outer column is not.
func (b *builder) walkSubLink(sl *pgast.SubLink, sc *scope, predicate bool) {
	if sl == nil {
		return
	}

	// x IN (SELECT ...) probes the outer column by equality. The grammar leaves
	// operName empty for IN and fills it for an explicit = ANY.
	if predicate && sl.GetSubLinkType() == pgast.SubLinkType_ANY_SUBLINK {
		op := lastName(sl.GetOperName())
		if op == "" || isEqualOp(op) {
			if column, ok := columnOf(sl.GetTestexpr()); ok {
				b.addUsage(sc, column, RoleEquality, false, 0)
			}
		}
	}

	b.walkStmt(sl.GetSubselect(), sc)
}

func (b *builder) walkSort(items []*pgast.Node, sc *scope) {
	for i, item := range items {
		sb := item.GetSortBy()
		if sb == nil {
			continue
		}

		column, ok := columnOf(sb.GetNode())
		if !ok {
			continue
		}

		b.addUsage(sc, column, RoleOrder, sb.GetSortbyDir() == pgast.SortByDir_SORTBY_DESC, i)
	}
}

func (b *builder) walkGroup(items []*pgast.Node, sc *scope) {
	for i, item := range items {
		column, ok := columnOf(item)
		if !ok {
			continue
		}

		b.addUsage(sc, column, RoleGroup, false, i)
	}
}

func (b *builder) addUsage(sc *scope, c colRef, role Role, desc bool, seq int) {
	var ref Ref

	if c.qualifier != "" {
		r, ok := sc.resolve(c.qualifier)
		if !ok {
			return // a CTE, a subquery alias, or a name out of scope
		}

		ref = r
	} else if r, ok := sc.sole(); ok {
		ref = r
	}
	// Otherwise Ref stays empty: with several tables in scope the owner of a bare
	// column is a catalog question, and guessing it would invent an index.

	b.addUsageRef(ref, c.name, role, desc, seq)
}

func (b *builder) addUsageRef(ref Ref, column string, role Role, desc bool, seq int) {
	u := Usage{Ref: ref, Column: column, Role: role, Desc: desc, Seq: seq}
	if _, ok := b.seenUsage[u]; ok {
		return
	}

	b.seenUsage[u] = struct{}{}
	b.stmt.Usages = append(b.stmt.Usages, u)
}

func (b *builder) noteWrappedColumn(nodes ...*pgast.Node) {
	for _, n := range nodes {
		if hasColumnRef(n) {
			b.note(ReasonExpressionPredicate)

			return
		}
	}
}

func refOf(rv *pgast.RangeVar) Ref {
	return Ref{
		Schema: rv.GetSchemaname(),
		Name:   rv.GetRelname(),
		Alias:  rv.GetAlias().GetAliasname(),
	}
}

// tableOfItem reports the table a JOIN side is, when that side is a plain table
// rather than a nested join or a subquery.
func tableOfItem(n *pgast.Node, sc *scope) (Ref, bool) {
	rv := n.GetRangeVar()
	if rv == nil {
		return Ref{}, false
	}

	if rv.GetSchemaname() == "" && sc.isCTE(rv.GetRelname()) {
		return Ref{}, false
	}

	return refOf(rv), true
}

func columnOf(n *pgast.Node) (colRef, bool) {
	ref := n.GetColumnRef()
	if ref == nil {
		return colRef{}, false
	}

	fields := ref.GetFields()
	if len(fields) == 0 {
		return colRef{}, false
	}

	name := fields[len(fields)-1].GetString_().GetSval()
	if name == "" {
		return colRef{}, false // t.* and other non-string tails
	}

	c := colRef{name: name}
	if len(fields) > 1 {
		c.qualifier = fields[len(fields)-2].GetString_().GetSval()
	}

	return c, true
}

func lastName(names []*pgast.Node) string {
	if len(names) == 0 {
		return ""
	}

	return names[len(names)-1].GetString_().GetSval()
}

func isEqualOp(op string) bool { return op == "=" }

func isRangeOp(op string) bool {
	switch op {
	case "<", "<=", ">", ">=":
		return true
	default:
		return false
	}
}

func (b *builder) walkSubLinks(n *pgast.Node, sc *scope) {
	visit(n, func(m proto.Message) bool {
		sl, ok := m.(*pgast.SubLink)
		if !ok {
			return true
		}

		b.walkSubLink(sl, sc, false)

		return false // walkSubLink descends into the subquery on its own terms
	})
}

func hasColumnRef(n *pgast.Node) bool {
	found := false

	visit(n, func(m proto.Message) bool {
		if _, ok := m.(*pgast.ColumnRef); ok {
			found = true
		}

		return !found
	})

	return found
}

// visit walks a message tree generically, calling fn on each message; fn returns
// false to stop the descent below it. Protobuf reflection is what makes it
// possible to find one node type inside an expression without spelling out the
// two hundred node types libpg_query can nest it under.
func visit(msg proto.Message, fn func(proto.Message) bool) {
	if msg == nil {
		return
	}

	m := msg.ProtoReflect()
	if !m.IsValid() { // a typed nil pointer
		return
	}

	if !fn(msg) {
		return
	}

	m.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if fd.Kind() != protoreflect.MessageKind || fd.IsMap() {
			return true
		}

		if fd.IsList() {
			items := v.List()
			for i := range items.Len() {
				visit(items.Get(i).Message().Interface(), fn)
			}

			return true
		}

		visit(v.Message().Interface(), fn)

		return true
	})
}

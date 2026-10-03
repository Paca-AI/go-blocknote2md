package blocknote2md

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Stage 3: mdast -> markdown, as mdast-util-to-markdown with the extensions
// remark-gfm adds (strikethrough, tables, task list items; the autolink and
// footnote extensions only contribute "unsafe" patterns) and remark-stringify's
// defaults. BlockNote overrides the text handler to emit raw values, which is
// why ordinary text is never escaped.

type mdInfo struct{ before, after string }

type attentionInfo struct{ before, after bool }

type mdState struct {
	stack          []string
	indexStack     []int
	bulletCurrent  string
	bulletLastUsed string
	attention      *attentionInfo
}

func (s *mdState) enter(name string) func() {
	s.stack = append(s.stack, name)
	return func() { s.stack = s.stack[:len(s.stack)-1] }
}

func (s *mdState) inStack(name string) bool {
	for _, n := range s.stack {
		if n == name {
			return true
		}
	}
	return false
}

// hastToMarkdown is the whole of cleanHTMLToMarkdown after the HTML exists.
func hastToMarkdown(root *hNode) string {
	tree := htmlToMdast(root)
	s := &mdState{}
	result := s.handle(tree, nil, mdInfo{before: "\n", after: "\n"})
	if result != "" && !strings.HasSuffix(result, "\n") && !strings.HasSuffix(result, "\r") {
		result += "\n"
	}
	return finalizeString(result)
}

func (s *mdState) handle(n, parent *mNode, in mdInfo) string {
	switch n.typ {
	case "root":
		for _, c := range n.children {
			if mdastPhrasing(c) {
				return s.containerPhrasing(n, in)
			}
		}
		return s.containerFlow(n, in)
	case "paragraph":
		exit := s.enter("paragraph")
		sub := s.enter("phrasing")
		v := s.containerPhrasing(n, in)
		sub()
		exit()
		return v
	case "heading":
		return s.heading(n, in)
	case "blockquote":
		exit := s.enter("blockquote")
		v := indentLines(s.containerFlow(n, in), func(line string, _ int, blank bool) string {
			if blank {
				return ">" + line
			}
			return "> " + line
		})
		exit()
		return v
	case "break":
		return s.hardBreak(in)
	case "code":
		return s.code(n)
	case "thematicBreak":
		return "***"
	case "list":
		return s.list(n, parent, in)
	case "listItem":
		return s.listItem(n, parent, in)
	case "emphasis":
		return s.emphasisOrStrong(n, in, "*")
	case "strong":
		return s.emphasisOrStrong(n, in, "**")
	case "delete":
		exit := s.enter("strikethrough")
		v := "~~"
		v += s.containerPhrasing(n, mdInfo{before: v, after: "~"})
		v += "~~"
		exit()
		return v
	case "inlineCode":
		v := inlineCodeValue(n.value, s)
		if s.inStack("tableCell") {
			v = strings.ReplaceAll(v, "|", `\|`)
		}
		return v
	case "link":
		return s.link(n, in)
	case "image":
		return s.image(n)
	case "text":
		return n.value // BlockNote's override: raw, never escaped
	case "table":
		return s.table(n, in)
	case "tableCell":
		exit := s.enter("tableCell")
		sub := s.enter("phrasing")
		// remark-gfm's tableCellPadding is unset, so the cell sees "|" on both sides.
		v := s.containerPhrasing(n, mdInfo{before: "|", after: "|"})
		sub()
		exit()
		return v
	}
	return ""
}

// ---- containers ----

func (s *mdState) containerFlow(parent *mNode, in mdInfo) string {
	s.indexStack = append(s.indexStack, -1)
	var sb strings.Builder
	for i, child := range parent.children {
		s.indexStack[len(s.indexStack)-1] = i
		sb.WriteString(s.handle(child, parent, mdInfo{before: "\n", after: "\n"}))
		if child.typ != "list" {
			s.bulletLastUsed = ""
		}
		if i < len(parent.children)-1 {
			sb.WriteString(between(child, parent.children[i+1], parent))
		}
	}
	s.indexStack = s.indexStack[:len(s.indexStack)-1]
	return sb.String()
}

// between is the blank space mdast-util-to-markdown's joinDefaults puts
// between two siblings. Only lists and list items carry a `spread` flag: a
// tight one joins with a single newline, except that a paragraph is always
// followed by a blank line before another paragraph or a setext heading.
func between(left, right, parent *mNode) string {
	if parent.typ != "list" && parent.typ != "listItem" {
		return "\n\n"
	}
	if left.typ == "paragraph" && (right.typ == "paragraph" || (right.typ == "heading" && formatHeadingAsSetext(right))) {
		return "\n\n"
	}
	if parent.spread {
		return "\n\n"
	}
	return "\n"
}

func (s *mdState) containerPhrasing(parent *mNode, in mdInfo) string {
	s.indexStack = append(s.indexStack, -1)
	var results []string
	before := in.before
	encodeAfter := ""
	hasEncodeAfter := false

	for i, child := range parent.children {
		s.indexStack[len(s.indexStack)-1] = i
		var after string
		if i+1 < len(parent.children) {
			after = s.peekFirst(parent.children[i+1], parent)
		} else {
			after = in.after
		}

		value := s.handle(child, parent, mdInfo{before: before, after: after})
		if hasEncodeAfter && encodeAfter == sliceFirstUnit(value) {
			value = encodeCharRef(firstUnit(encodeAfter)) + dropFirstUnit(value)
		}
		enc := s.attention
		s.attention = nil
		hasEncodeAfter = false
		if enc != nil {
			if len(results) > 0 && enc.before && before == sliceLastUnit(results[len(results)-1]) {
				last := results[len(results)-1]
				results[len(results)-1] = dropLastUnit(last) + encodeCharRef(firstUnit(before))
			}
			if enc.after {
				encodeAfter = after
				hasEncodeAfter = true
			}
		}
		results = append(results, value)
		before = sliceLastUnit(value)
	}
	s.indexStack = s.indexStack[:len(s.indexStack)-1]
	return strings.Join(results, "")
}

// peekFirst is charAt(0) of what the next sibling will serialize to; the
// inline handlers have a cheap "peek", the rest are run.
func (s *mdState) peekFirst(n, parent *mNode) string {
	switch n.typ {
	case "emphasis", "strong":
		return "*"
	case "inlineCode":
		return "`"
	case "delete":
		return "~"
	case "image":
		return "!"
	case "link":
		if formatLinkAsAutolink(n) {
			return "<"
		}
		return "["
	}
	return sliceFirstUnit(s.handle(n, parent, mdInfo{}))
}

// ---- block handlers ----

func (s *mdState) heading(n *mNode, in mdInfo) string {
	rank := n.depth
	if rank < 1 {
		rank = 1
	}
	if rank > 6 {
		rank = 6
	}
	if formatHeadingAsSetext(n) {
		exit := s.enter("headingSetext")
		sub := s.enter("phrasing")
		value := s.containerPhrasing(n, mdInfo{before: "\n", after: "\n"})
		sub()
		exit()
		marker := "-"
		if rank == 1 {
			marker = "="
		}
		lastBreak := strings.LastIndexAny(value, "\r\n")
		return value + "\n" + strings.Repeat(marker, jsLen(value[lastBreak+1:]))
	}
	sequence := strings.Repeat("#", rank)
	exit := s.enter("headingAtx")
	sub := s.enter("phrasing")
	value := s.containerPhrasing(n, mdInfo{before: "# ", after: "\n"})
	if value != "" && (value[0] == ' ' || value[0] == '\t') {
		value = encodeCharRef(int(value[0])) + value[1:]
	}
	if value != "" {
		value = sequence + " " + value
	} else {
		value = sequence
	}
	sub()
	exit()
	return value
}

func formatHeadingAsSetext(n *mNode) bool {
	literalWithBreak := false
	var walk func(*mNode)
	walk = func(x *mNode) {
		if literalWithBreak {
			return
		}
		if x.typ == "break" || strings.ContainsAny(x.value, "\r\n") {
			literalWithBreak = true
			return
		}
		for _, c := range x.children {
			walk(c)
		}
	}
	walk(n)
	return (n.depth == 0 || n.depth < 3) && mdastToString(n) != "" && literalWithBreak
}

func mdastToString(n *mNode) string {
	if n.value != "" || n.typ == "text" || n.typ == "inlineCode" || n.typ == "code" {
		return n.value
	}
	if n.typ == "image" && n.alt != "" {
		return n.alt
	}
	var sb strings.Builder
	for _, c := range n.children {
		sb.WriteString(mdastToString(c))
	}
	return sb.String()
}

func (s *mdState) hardBreak(in mdInfo) string {
	for _, p := range unsafePatterns {
		if p.character == "\n" && patternInScope(s.stack, p) {
			if strings.ContainsAny(in.before, " \t") {
				return ""
			}
			return " "
		}
	}
	return "\\\n"
}

func (s *mdState) code(n *mNode) string {
	raw := n.value
	sequence := strings.Repeat("`", maxInt(longestStreak(raw, "`")+1, 3))
	exit := s.enter("codeFenced")
	value := sequence
	if n.lang != "" {
		sub := s.enter("codeFencedLangGraveAccent")
		value += s.safe(n.lang, value, " ", []string{"`"})
		sub()
	}
	value += "\n"
	if raw != "" {
		value += raw + "\n"
	}
	value += sequence
	exit()
	return value
}

func longestStreak(value, sub string) int {
	index := strings.Index(value, sub)
	expected := index
	count, max := 0, 0
	for index != -1 {
		if index == expected {
			count++
			if count > max {
				max = count
			}
		} else {
			count = 1
		}
		expected = index + len(sub)
		next := strings.Index(value[expected:], sub)
		if next == -1 {
			break
		}
		index = expected + next
	}
	return max
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (s *mdState) list(n, parent *mNode, in mdInfo) string {
	exit := s.enter("list")
	bulletCurrent := s.bulletCurrent
	bullet := "*"
	bulletOther := "-"
	if n.ordered {
		bullet = "."
		bulletOther = ")"
	}
	useDifferent := parent != nil && s.bulletLastUsed != "" && bullet == s.bulletLastUsed

	if !n.ordered {
		var first *mNode
		if len(n.children) > 0 {
			first = n.children[0]
		}
		st, ix := s.stack, s.indexStack
		if (bullet == "*" || bullet == "-") && first != nil && len(first.children) == 0 &&
			len(st) >= 4 && st[len(st)-1] == "list" && st[len(st)-2] == "listItem" &&
			st[len(st)-3] == "list" && st[len(st)-4] == "listItem" &&
			len(ix) >= 3 && ix[len(ix)-1] == 0 && ix[len(ix)-2] == 0 && ix[len(ix)-3] == 0 {
			useDifferent = true
		}
		// The rule marker is "*": a bullet list whose first item opens with a
		// thematic break would read as one.
		if first != nil && bullet == "*" {
			for _, item := range n.children {
				if item.typ == "listItem" && len(item.children) > 0 && item.children[0].typ == "thematicBreak" {
					useDifferent = true
					break
				}
			}
		}
	}
	if useDifferent {
		bullet = bulletOther
	}
	s.bulletCurrent = bullet
	value := s.containerFlow(n, in)
	s.bulletLastUsed = bullet
	s.bulletCurrent = bulletCurrent
	exit()
	return value
}

var taskBullet = regexp.MustCompile(`^(?:[*+-]|\d+\.)([\r\n]| {1,3})`)

func (s *mdState) listItem(n, parent *mNode, in mdInfo) string {
	bullet := s.bulletCurrent
	if bullet == "" {
		bullet = "*"
	}
	if parent != nil && parent.typ == "list" && parent.ordered {
		start := 1
		if parent.start > -1 {
			start = parent.start
		}
		idx := 0
		for i, c := range parent.children {
			if c == n {
				idx = i
				break
			}
		}
		bullet = strconv.Itoa(start+idx) + bullet
	}
	size := len(bullet) + 1

	exit := s.enter("listItem")
	value := indentLines(s.containerFlow(n, in), func(line string, index int, blank bool) string {
		if index > 0 {
			if blank {
				return ""
			}
			return strings.Repeat(" ", size) + line
		}
		if blank {
			return bullet
		}
		return bullet + strings.Repeat(" ", size-len(bullet)) + line
	})
	exit()

	// remark-gfm's task list item handler: a checkable item gets its checkbox
	// right after the bullet.
	if n.checked != nil && len(n.children) > 0 && n.children[0].typ == "paragraph" {
		box := "[ ] "
		if *n.checked {
			box = "[x] "
		}
		if loc := taskBullet.FindStringIndex(value); loc != nil {
			value = value[:loc[1]] + box + value[loc[1]:]
		}
	}
	return value
}

var eolRe = regexp.MustCompile(`\r?\n|\r`)

func indentLines(value string, fn func(line string, index int, blank bool) string) string {
	var sb strings.Builder
	start, line := 0, 0
	for _, m := range eolRe.FindAllStringIndex(value, -1) {
		l := value[start:m[0]]
		sb.WriteString(fn(l, line, l == ""))
		sb.WriteString(value[m[0]:m[1]])
		start = m[1]
		line++
	}
	l := value[start:]
	sb.WriteString(fn(l, line, l == ""))
	return sb.String()
}

// ---- phrasing handlers ----

func (s *mdState) emphasisOrStrong(n *mNode, in mdInfo, mark string) string {
	marker := mark[:1]
	name := "emphasis"
	if mark == "**" {
		name = "strong"
	}
	exit := s.enter(name)
	before := mark
	between := s.containerPhrasing(n, mdInfo{before: before, after: marker})

	betweenHead := firstUnit(between)
	open := encodeInfo(lastUnit(in.before), betweenHead, marker)
	if open.inside {
		between = encodeCharRef(betweenHead) + dropFirstUnit(between)
	}
	betweenTail := lastUnit(between)
	closing := encodeInfo(firstUnit(in.after), betweenTail, marker)
	if closing.inside {
		between = dropLastUnit(between) + encodeCharRef(betweenTail)
	}
	exit()
	s.attention = &attentionInfo{before: open.outside, after: closing.outside}
	return mark + between + mark
}

type encInfo struct{ inside, outside bool }

// encodeInfo decides which characters next to an emphasis marker must be
// written as character references so the marker still forms emphasis.
func encodeInfo(outside, inside int, marker string) encInfo {
	outsideKind := classifyChar(outside)
	insideKind := classifyChar(inside)
	if outsideKind == 0 {
		if insideKind == 0 {
			if marker == "_" {
				return encInfo{true, true}
			}
			return encInfo{false, false}
		}
		if insideKind == 1 {
			return encInfo{true, true}
		}
		return encInfo{false, true}
	}
	if outsideKind == 1 {
		if insideKind == 0 {
			return encInfo{false, false}
		}
		if insideKind == 1 {
			return encInfo{true, true}
		}
		return encInfo{false, false}
	}
	if insideKind == 0 {
		return encInfo{false, false}
	}
	if insideKind == 1 {
		return encInfo{true, false}
	}
	return encInfo{false, false}
}

// classifyChar is micromark's classifyCharacter: 1 whitespace, 2 punctuation,
// 0 anything else (including "no character").
func classifyChar(code int) int {
	if code == ' ' || jsSpace(code) {
		return 1
	}
	if jsPunctuation(code) {
		return 2
	}
	return 0
}

// encodeCharRef is encodeCharacterReference. A negative code stands for NaN
// (charCodeAt on an empty string), which the library really does write as
// "&#xNAN;" — reproduced because the goal is BlockNote's exact output.
func encodeCharRef(code int) string {
	if code < 0 {
		return "&#xNAN;"
	}
	return "&#x" + strings.ToUpper(strconv.FormatInt(int64(code), 16)) + ";"
}

func inlineCodeValue(value string, s *mdState) string {
	sequence := "`"
	for regexp.MustCompile("(^|[^`])" + sequence + "([^`]|$)").MatchString(value) {
		sequence += "`"
	}
	if strings.IndexFunc(value, func(r rune) bool { return r != ' ' && r != '\r' && r != '\n' }) >= 0 &&
		((startsWithAny(value, " \r\n") && endsWithAny(value, " \r\n")) || strings.HasPrefix(value, "`") || strings.HasSuffix(value, "`")) {
		value = " " + value + " "
	}
	for _, p := range unsafePatterns {
		if !p.atBreak {
			continue
		}
		re := p.compile()
		for {
			m := re.FindStringIndex(value)
			if m == nil {
				break
			}
			pos := m[0]
			if value[pos] == '\n' && pos > 0 && value[pos-1] == '\r' {
				pos--
			}
			value = value[:pos] + " " + value[m[0]+1:]
			// A replaced match can no longer match; keep scanning from the
			// start for the next one, as exec() would from lastIndex.
			if !re.MatchString(value) {
				break
			}
		}
	}
	return sequence + value + sequence
}

func startsWithAny(s, set string) bool { return s != "" && strings.ContainsRune(set, rune(s[0])) }
func endsWithAny(s, set string) bool   { return s != "" && strings.ContainsRune(set, rune(s[len(s)-1])) }

var autolinkScheme = regexp.MustCompile(`(?i)^[a-z][a-z+.-]+:`)

func formatLinkAsAutolink(n *mNode) bool {
	raw := mdastToString(n)
	return n.url != "" && n.title == "" && len(n.children) == 1 && n.children[0].typ == "text" &&
		(raw == n.url || "mailto:"+raw == n.url) &&
		autolinkScheme.MatchString(n.url) &&
		!strings.ContainsFunc(n.url, func(r rune) bool { return r <= ' ' || r == '<' || r == '>' || r == 0x7F })
}

func needsLiteralDestination(url string) bool {
	return strings.ContainsFunc(url, func(r rune) bool { return r <= ' ' || r == 0x7F })
}

func (s *mdState) link(n *mNode, in mdInfo) string {
	if formatLinkAsAutolink(n) {
		saved := s.stack
		s.stack = nil
		exit := s.enter("autolink")
		value := "<"
		value += s.containerPhrasing(n, mdInfo{before: value, after: ">"})
		value += ">"
		exit()
		s.stack = saved
		return value
	}
	exit := s.enter("link")
	sub := s.enter("label")
	value := "["
	value += s.containerPhrasing(n, mdInfo{before: value, after: "]("})
	value += "]("
	sub()
	value += s.destination(n, value)
	value += ")"
	exit()
	return value
}

func (s *mdState) image(n *mNode) string {
	exit := s.enter("image")
	sub := s.enter("label")
	value := "!["
	value += s.safe(n.alt, value, "]", nil)
	value += "]("
	sub()
	value += s.destination(n, value)
	value += ")"
	exit()
	return value
}

// destination writes a link/image URL: in angle brackets when it holds spaces
// or control characters. (Titles never occur in BlockNote output.)
func (s *mdState) destination(n *mNode, before string) string {
	if needsLiteralDestination(n.url) {
		sub := s.enter("destinationLiteral")
		v := "<" + s.safe(n.url, before+"<", ">", nil) + ">"
		sub()
		return v
	}
	sub := s.enter("destinationRaw")
	v := s.safe(n.url, before, ")", nil)
	sub()
	return v
}

// ---- tables ----

func (s *mdState) table(n *mNode, in mdInfo) string {
	exit := s.enter("table")
	matrix := make([][]string, len(n.children))
	for i, row := range n.children {
		sub := s.enter("tableRow")
		cells := make([]string, len(row.children))
		for j, cell := range row.children {
			cells[j] = s.handle(cell, row, in)
		}
		sub()
		matrix[i] = cells
	}
	exit()
	return markdownTable(matrix, n.align)
}

// markdownTable is markdown-table with remark-gfm's settings: padded cells and
// aligned delimiters, lengths counted in UTF-16 code units.
func markdownTable(table [][]string, align []string) string {
	var cellMatrix [][]string
	var sizeMatrix [][]int
	var longest []int
	mostCells := 0

	for _, row := range table {
		if len(row) > mostCells {
			mostCells = len(row)
		}
		sizes := make([]int, len(row))
		for ci, cell := range row {
			size := jsLen(cell)
			sizes[ci] = size
			for len(longest) <= ci {
				longest = append(longest, -1)
			}
			if size > longest[ci] {
				longest[ci] = size
			}
		}
		cellMatrix = append(cellMatrix, row)
		sizeMatrix = append(sizeMatrix, sizes)
	}
	for len(longest) < mostCells {
		longest = append(longest, 0)
	}

	alignments := make([]byte, mostCells)
	for i := 0; i < mostCells; i++ {
		if i < len(align) && align[i] != "" {
			switch align[i][0] {
			case 'c', 'C':
				alignments[i] = 'c'
			case 'l', 'L':
				alignments[i] = 'l'
			case 'r', 'R':
				alignments[i] = 'r'
			}
		}
	}

	delimiter := make([]string, mostCells)
	delimiterSizes := make([]int, mostCells)
	for i := 0; i < mostCells; i++ {
		before, after := "", ""
		switch alignments[i] {
		case 'c':
			before, after = ":", ":"
		case 'l':
			before = ":"
		case 'r':
			after = ":"
		}
		size := maxInt(1, longest[i]-len(before)-len(after))
		delimiter[i] = before + strings.Repeat("-", size) + after
		size = len(before) + size + len(after)
		if size > longest[i] {
			longest[i] = size
		}
		delimiterSizes[i] = size
	}
	cellMatrix = append(cellMatrix[:1], append([][]string{delimiter}, cellMatrix[1:]...)...)
	sizeMatrix = append(sizeMatrix[:1], append([][]int{delimiterSizes}, sizeMatrix[1:]...)...)

	var lines []string
	for ri, row := range cellMatrix {
		sizes := sizeMatrix[ri]
		var line strings.Builder
		for ci := 0; ci < mostCells; ci++ {
			cell := ""
			if ci < len(row) {
				cell = row[ci]
			}
			size := 0
			if ci < len(sizes) {
				size = sizes[ci]
			}
			gap := longest[ci] - size
			beforePad, afterPad := "", ""
			switch alignments[ci] {
			case 'r':
				beforePad = strings.Repeat(" ", gap)
			case 'c':
				if gap%2 == 1 {
					beforePad = strings.Repeat(" ", gap/2+1)
					afterPad = strings.Repeat(" ", gap/2)
				} else {
					beforePad = strings.Repeat(" ", gap/2)
					afterPad = beforePad
				}
			default:
				afterPad = strings.Repeat(" ", gap)
			}
			if ci == 0 {
				line.WriteString("|")
			}
			line.WriteString(" ")
			line.WriteString(beforePad)
			line.WriteString(cell)
			line.WriteString(afterPad)
			line.WriteString(" ")
			line.WriteString("|")
		}
		lines = append(lines, line.String())
	}
	return strings.Join(lines, "\n")
}

// ---- unsafe patterns and safe() ----

type unsafePattern struct {
	character      string
	before, after  string
	atBreak        bool
	inConstruct    []string
	notInConstruct []string
	re             *regexp.Regexp
}

func (p *unsafePattern) compile() *regexp.Regexp {
	if p.re != nil {
		return p.re
	}
	before := ""
	if p.atBreak {
		before += `[\r\n][\t ]*`
	}
	if p.before != "" {
		before += "(?:" + p.before + ")"
	}
	src := ""
	if before != "" {
		src = "(" + before + ")"
	}
	if strings.ContainsAny(p.character, `|\{}()[]^$+*?.-`) {
		src += `\`
	}
	src += p.character
	if p.after != "" {
		src += "(?:" + p.after + ")"
	}
	p.re = regexp.MustCompile(src)
	return p.re
}

func listInScope(stack, list []string, none bool) bool {
	if len(list) == 0 {
		return none
	}
	for _, want := range list {
		for _, have := range stack {
			if have == want {
				return true
			}
		}
	}
	return false
}

func patternInScope(stack []string, p *unsafePattern) bool {
	return listInScope(stack, p.inConstruct, true) && !listInScope(stack, p.notInConstruct, false)
}

var fullPhrasingSpans = []string{"autolink", "destinationLiteral", "destinationRaw", "reference", "titleQuote", "titleApostrophe"}

// unsafePatterns is mdast-util-to-markdown's unsafe list followed by the
// patterns remark-gfm's extensions add, in the library's order.
var unsafePatterns = []*unsafePattern{
	{character: "\t", after: `[\r\n]`, inConstruct: []string{"phrasing"}},
	{character: "\t", before: `[\r\n]`, inConstruct: []string{"phrasing"}},
	{character: "\t", inConstruct: []string{"codeFencedLangGraveAccent", "codeFencedLangTilde"}},
	{character: "\r", inConstruct: []string{"codeFencedLangGraveAccent", "codeFencedLangTilde", "codeFencedMetaGraveAccent", "codeFencedMetaTilde", "destinationLiteral", "headingAtx"}},
	{character: "\n", inConstruct: []string{"codeFencedLangGraveAccent", "codeFencedLangTilde", "codeFencedMetaGraveAccent", "codeFencedMetaTilde", "destinationLiteral", "headingAtx"}},
	{character: " ", after: `[\r\n]`, inConstruct: []string{"phrasing"}},
	{character: " ", before: `[\r\n]`, inConstruct: []string{"phrasing"}},
	{character: " ", inConstruct: []string{"codeFencedLangGraveAccent", "codeFencedLangTilde"}},
	{character: "!", after: `\[`, inConstruct: []string{"phrasing"}, notInConstruct: fullPhrasingSpans},
	{character: `"`, inConstruct: []string{"titleQuote"}},
	{atBreak: true, character: "#"},
	{character: "#", inConstruct: []string{"headingAtx"}, after: "(?:[\r\n]|$)"},
	{character: "&", after: `[#A-Za-z]`, inConstruct: []string{"phrasing"}},
	{character: "'", inConstruct: []string{"titleApostrophe"}},
	{character: "(", inConstruct: []string{"destinationRaw"}},
	{before: `\]`, character: "(", inConstruct: []string{"phrasing"}, notInConstruct: fullPhrasingSpans},
	{atBreak: true, before: `\d+`, character: ")"},
	{character: ")", inConstruct: []string{"destinationRaw"}},
	{atBreak: true, character: "*", after: "(?:[ \t\r\n*])"},
	{character: "*", inConstruct: []string{"phrasing"}, notInConstruct: fullPhrasingSpans},
	{atBreak: true, character: "+", after: "(?:[ \t\r\n])"},
	{atBreak: true, character: "-", after: "(?:[ \t\r\n-])"},
	{atBreak: true, before: `\d+`, character: ".", after: "(?:[ \t\r\n]|$)"},
	{atBreak: true, character: "<", after: `[!/?A-Za-z]`},
	{character: "<", after: `[!/?A-Za-z]`, inConstruct: []string{"phrasing"}, notInConstruct: fullPhrasingSpans},
	{character: "<", inConstruct: []string{"destinationLiteral"}},
	{atBreak: true, character: "="},
	{atBreak: true, character: ">"},
	{character: ">", inConstruct: []string{"destinationLiteral"}},
	{atBreak: true, character: "["},
	{character: "[", inConstruct: []string{"phrasing"}, notInConstruct: fullPhrasingSpans},
	{character: "[", inConstruct: []string{"label", "reference"}},
	{character: `\`, after: `[\r\n]`, inConstruct: []string{"phrasing"}},
	{character: "]", inConstruct: []string{"label", "reference"}},
	{atBreak: true, character: "_"},
	{character: "_", inConstruct: []string{"phrasing"}, notInConstruct: fullPhrasingSpans},
	{atBreak: true, character: "`"},
	{character: "`", inConstruct: []string{"codeFencedLangGraveAccent", "codeFencedMetaGraveAccent"}},
	{character: "`", inConstruct: []string{"phrasing"}, notInConstruct: fullPhrasingSpans},
	{atBreak: true, character: "~"},
	// gfm autolink literal
	{character: "@", before: `[+\-.\w]`, after: `[\-.\w]`, inConstruct: []string{"phrasing"}, notInConstruct: []string{"autolink", "link", "image", "label"}},
	{character: ".", before: `[Ww]`, after: `[\-.\w]`, inConstruct: []string{"phrasing"}, notInConstruct: []string{"autolink", "link", "image", "label"}},
	{character: ":", before: `[ps]`, after: `\/`, inConstruct: []string{"phrasing"}, notInConstruct: []string{"autolink", "link", "image", "label"}},
	// gfm footnote
	{character: "[", inConstruct: []string{"label", "phrasing", "reference"}},
	// gfm strikethrough
	{character: "~", inConstruct: []string{"phrasing"}, notInConstruct: fullPhrasingSpans},
	// gfm table
	{character: "\r", inConstruct: []string{"tableCell"}},
	{character: "\n", inConstruct: []string{"tableCell"}},
	{atBreak: true, character: "|", after: "[\t :-]"},
	{character: "|", inConstruct: []string{"tableCell"}},
	{atBreak: true, character: ":", after: "-"},
	{atBreak: true, character: "-", after: "[:|-]"},
	// gfm task list item
	{atBreak: true, character: "-", after: "[:|-]"},
}

func isASCIIPunct(b byte) bool {
	return (b >= '!' && b <= '/') || (b >= ':' && b <= '@') || (b >= '[' && b <= '`') || (b >= '{' && b <= '~')
}

// safe is mdast-util-to-markdown's safe(): it escapes the characters of input
// that would be read as syntax in the current construct. before/after are the
// surrounding output the escaping decisions depend on.
func (s *mdState) safe(input, before, after string, encode []string) string {
	value := before + input + after
	var positions []int
	type posInfo struct{ before, after bool }
	infos := map[int]*posInfo{}

	for _, p := range unsafePatterns {
		if !patternInScope(s.stack, p) {
			continue
		}
		re := p.compile()
		for _, m := range re.FindAllStringSubmatchIndex(value, -1) {
			hasBefore := p.before != "" || p.atBreak
			hasAfter := p.after != ""
			position := m[0]
			if hasBefore {
				position += m[3] - m[2]
			}
			if cur, ok := infos[position]; ok {
				if cur.before && !hasBefore {
					cur.before = false
				}
				if cur.after && !hasAfter {
					cur.after = false
				}
			} else {
				positions = append(positions, position)
				infos[position] = &posInfo{hasBefore, hasAfter}
			}
		}
	}
	sort.Ints(positions)

	var result []string
	start := len(before)
	end := len(value) - len(after)
	for i, position := range positions {
		if position < start || position >= end {
			continue
		}
		if (position+1 < end && i+1 < len(positions) && positions[i+1] == position+1 &&
			infos[position].after && !infos[position+1].before && !infos[position+1].after) ||
			(i > 0 && positions[i-1] == position-1 && infos[position].before &&
				!infos[position-1].before && !infos[position-1].after) {
			continue
		}
		if start != position {
			result = append(result, escapeBackslashes(value[start:position], "\\"))
		}
		start = position
		ch := value[position]
		encoded := false
		for _, e := range encode {
			if e == string(ch) {
				encoded = true
			}
		}
		if isASCIIPunct(ch) && !encoded {
			result = append(result, "\\")
		} else {
			result = append(result, encodeCharRef(int(ch)))
			start++
		}
	}
	result = append(result, escapeBackslashes(value[start:end], after))
	return strings.Join(result, "")
}

// escapeBackslashes doubles a backslash that is followed by ASCII punctuation,
// looking one character into `after` for the last one.
func escapeBackslashes(value, after string) string {
	whole := value + after
	var positions []int
	for i := 0; i < len(whole)-1; i++ {
		if whole[i] == '\\' && isASCIIPunct(whole[i+1]) {
			positions = append(positions, i)
		}
	}
	var results []string
	start := 0
	for _, p := range positions {
		if start != p {
			results = append(results, safeSlice(value, start, p))
		}
		results = append(results, "\\")
		start = p
	}
	results = append(results, safeSlice(value, start, len(value)))
	return strings.Join(results, "")
}

func safeSlice(s string, from, to int) string {
	if from > len(s) {
		from = len(s)
	}
	if to > len(s) {
		to = len(s)
	}
	if from >= to {
		return ""
	}
	return s[from:to]
}

package blocknote2md

import (
	"regexp"
	"strconv"
	"strings"
)

// Stage 2: BlockNote's rehype plugins, then hast-util-to-mdast.

// ---- mdast ----

type mNode struct {
	typ      string
	children []*mNode
	value    string // text, inlineCode, code
	lang     string // code
	depth    int    // heading
	ordered  bool   // list
	start    int    // list (ordered)
	spread   bool   // list, listItem
	checked  *bool  // listItem
	url      string // link, image
	title    string // link, image
	alt      string // image
	align    []string
	spanInfo *cellSpan // tableCell: rowspan/colspan attributes seen on the <td>
}

func mText(v string) *mNode { return &mNode{typ: "text", value: v} }

var mdastPhrasingTypes = setOf(
	"break", "delete", "emphasis", "footnote", "footnoteReference", "image",
	"imageReference", "inlineCode", "inlineMath", "link", "linkReference",
	"mdxJsxTextElement", "mdxTextExpression", "strong", "text", "textDirective",
)

func mdastPhrasing(n *mNode) bool { return mdastPhrasingTypes[n.typ] }

// ---- BlockNote's rehype plugins (markdownExporter.ts) ----

// convertVideoToMarkdown replaces <video> with a text node `![name](src)`
// (raw, so it is never escaped).
func convertVideoToMarkdown(n *hNode) {
	for i, c := range n.children {
		if c.kind == hElement && c.tag == "video" {
			name := c.get("title")
			src := c.get("src")
			n.children[i] = hTextNode("![" + name + "](" + src + ")")
			continue
		}
		convertVideoToMarkdown(c)
	}
}

// removeUnderlines lifts the children of <u> into its parent.
func removeUnderlines(tree *hNode) {
	for i := 0; i < len(tree.children); i++ {
		node := tree.children[i]
		if node.kind != hElement {
			continue
		}
		removeUnderlines(node)
		if node.tag == "u" {
			rest := append([]*hNode{}, tree.children[i+1:]...)
			tree.children = append(append(tree.children[:i], node.children...), rest...)
			i += len(node.children) - 1
		}
	}
}

// addSpacesToCheckboxes turns the <p> after a checkbox into a <span> that
// starts with a space, so the text separates from the marker.
func addSpacesToCheckboxes(tree *hNode) {
	for i := len(tree.children) - 1; i >= 0; i-- {
		child := tree.children[i]
		var next *hNode
		if i+1 < len(tree.children) {
			next = tree.children[i+1]
		}
		if child.isEl("input") && child.get("type") == "checkbox" && next != nil && next.isEl("p") {
			next.tag = "span"
			next.children = append([]*hNode{hTextNode(" ")}, next.children...)
		} else if hasChildren(child) {
			addSpacesToCheckboxes(child)
		}
	}
}

// ---- hast-util-to-mdast ----

type toMdast struct {
	inTable bool
}

var ignoredTags = setOf(
	"applet", "area", "basefont", "bgsound", "caption", "col", "colgroup",
	"command", "content", "datalist", "dialog", "element", "embed", "frame",
	"frameset", "isindex", "keygen", "link", "math", "menu", "menuitem", "meta",
	"nextid", "noembed", "noframes", "optgroup", "option", "param", "script",
	"shadow", "source", "spacer", "style", "svg", "template", "title", "track",
	"base",
)

var passThroughTags = setOf(
	"abbr", "acronym", "bdi", "bdo", "big", "blink", "button", "canvas", "cite",
	"data", "details", "dfn", "font", "ins", "label", "map", "marquee", "meter",
	"nobr", "noscript", "object", "output", "progress", "rb", "rbc", "rp", "rt",
	"rtc", "ruby", "slot", "small", "span", "sup", "sub", "tbody", "tfoot",
	"thead", "time",
)

var flowTags = setOf(
	"address", "article", "aside", "body", "center", "div", "fieldset",
	"figcaption", "figure", "form", "footer", "header", "hgroup", "html",
	"legend", "main", "multicol", "nav", "picture", "section",
)

func (s *toMdast) all(parent *hNode) []*mNode {
	var results []*mNode
	for _, child := range parent.children {
		results = append(results, s.one(child, parent)...)
	}
	return results
}

func (s *toMdast) one(node, parent *hNode) []*mNode {
	switch node.kind {
	case hText:
		return []*mNode{mText(node.value)}
	case hRoot:
		return s.root(node)
	case hElement:
		// handled below
	}
	if h := s.element(node); h != nil || s.handled(node.tag) {
		return h
	}
	// No handler for the tag (an unknown <h7>, say): just its children.
	return s.all(node)
}

func (s *toMdast) handled(tag string) bool {
	if ignoredTags[tag] || passThroughTags[tag] || flowTags[tag] {
		return true
	}
	switch tag {
	case "a", "audio", "b", "blockquote", "br", "code", "dir", "dt", "dd", "del",
		"em", "h1", "h2", "h3", "h4", "h5", "h6", "hr", "i", "img", "image", "input",
		"kbd", "li", "listing", "mark", "ol", "p", "plaintext", "pre", "s", "samp",
		"strike", "strong", "summary", "table", "td", "th", "tr", "tt", "u", "ul",
		"var", "video", "xmp":
		return true
	}
	return false
}

// element dispatches an element to its handler; it returns nil both for
// handlers that produce nothing and for tags without one (see handled).
func (s *toMdast) element(n *hNode) []*mNode {
	tag := n.tag
	switch {
	case ignoredTags[tag]:
		return nil
	case passThroughTags[tag]:
		return s.all(n)
	case flowTags[tag]:
		return s.toFlow(s.all(n))
	}
	switch tag {
	case "a":
		return []*mNode{{typ: "link", url: n.get("href"), title: n.get("title"), children: s.all(n)}}
	case "audio", "video":
		return s.media(n)
	case "b", "strong":
		return []*mNode{{typ: "strong", children: s.all(n)}}
	case "i", "em", "mark", "u":
		return []*mNode{{typ: "emphasis", children: s.all(n)}}
	case "del", "s", "strike":
		return []*mNode{{typ: "delete", children: s.all(n)}}
	case "blockquote":
		return []*mNode{{typ: "blockquote", children: s.toFlow(s.all(n))}}
	case "br":
		return []*mNode{{typ: "break"}}
	case "hr":
		return []*mNode{{typ: "thematicBreak"}}
	case "code", "kbd", "samp", "tt", "var":
		return []*mNode{{typ: "inlineCode", value: toTextInline(n)}}
	case "pre", "listing", "plaintext", "xmp":
		return []*mNode{s.codeBlock(n)}
	case "h1", "h2", "h3", "h4", "h5", "h6":
		return []*mNode{{typ: "heading", depth: int(tag[1] - '0'), children: dropSurroundingBreaks(s.all(n))}}
	case "img", "image":
		return []*mNode{{typ: "image", url: n.get("src"), title: n.get("title"), alt: n.get("alt")}}
	case "input":
		return s.input(n)
	case "li", "dt", "dd":
		return []*mNode{s.listItem(n)}
	case "ul", "ol", "dir":
		return []*mNode{s.list(n)}
	case "p", "summary":
		children := dropSurroundingBreaks(s.all(n))
		if len(children) > 0 {
			return []*mNode{{typ: "paragraph", children: children}}
		}
		return nil
	case "table":
		return []*mNode{s.table(n)}
	case "td", "th":
		return []*mNode{s.tableCell(n)}
	case "tr":
		return []*mNode{{typ: "tableRow", children: s.toSpecificContent(s.all(n), "tableCell")}}
	}
	return nil
}

// root always wraps: rehype-remark sets the `document` option, so phrasing
// content at the top (a bare image, a link) becomes a paragraph.
func (s *toMdast) root(node *hNode) []*mNode {
	return []*mNode{{typ: "root", children: wrap(s.all(node))}}
}

func (s *toMdast) toFlow(nodes []*mNode) []*mNode { return wrap(nodes) }

// toSpecificContent regroups nodes so every one is a `want`; stray ones are
// gathered into an implicit container.
func (s *toMdast) toSpecificContent(nodes []*mNode, want string) []*mNode {
	var results []*mNode
	var queue []*mNode
	for _, node := range nodes {
		if node.typ == want {
			if len(queue) > 0 {
				node.children = append(append([]*mNode{}, queue...), node.children...)
				queue = nil
			}
			results = append(results, node)
		} else {
			queue = append(queue, node)
		}
	}
	if len(queue) > 0 {
		var last *mNode
		if len(results) > 0 {
			last = results[len(results)-1]
		} else {
			last = newContainer(want)
			results = append(results, last)
		}
		last.children = append(last.children, queue...)
	}
	return results
}

func newContainer(typ string) *mNode {
	if typ == "listItem" {
		return &mNode{typ: "listItem"}
	}
	return &mNode{typ: typ}
}

func dropSurroundingBreaks(nodes []*mNode) []*mNode {
	start, end := 0, len(nodes)
	for start < end && nodes[start].typ == "break" {
		start++
	}
	for end > start && nodes[end-1].typ == "break" {
		end--
	}
	return nodes[start:end]
}

// ---- wrap (util/wrap.js) ----

func wrapNeeded(nodes []*mNode) bool {
	for _, n := range nodes {
		if !mdastPhrasing(n) || (n.children != nil && wrapNeeded(n.children)) {
			return true
		}
	}
	return false
}

// wrap groups runs of phrasing nodes into paragraphs. (The library also splits
// links/deletes holding flow content; the exporter never produces those.)
func wrap(nodes []*mNode) []*mNode {
	var result []*mNode
	var queue []*mNode
	flush := func() {
		if len(queue) == 0 {
			return
		}
		allBlank := true
		for _, d := range queue {
			if d.typ != "text" || strings.Trim(d.value, " \t\n\f\r") != "" {
				allBlank = false
				break
			}
		}
		if !allBlank {
			result = append(result, &mNode{typ: "paragraph", children: dropSurroundingBreaks(queue)})
		}
		queue = nil
	}
	for _, n := range nodes {
		if mdastPhrasing(n) {
			queue = append(queue, n)
		} else {
			flush()
			result = append(result, n)
		}
	}
	flush()
	return result
}

// ---- handlers ----

func (s *toMdast) media(n *hNode) []*mNode {
	nodes := s.all(n)
	linkInFallback := false
	var walk func([]*mNode)
	walk = func(ns []*mNode) {
		for _, c := range ns {
			if c.typ == "link" {
				linkInFallback = true
			}
			walk(c.children)
		}
	}
	walk(nodes)
	if linkInFallback || wrapNeeded(nodes) {
		return nodes
	}
	source := n.get("src")
	for i := 0; source == "" && i < len(n.children); i++ {
		if c := n.children[i]; c.isEl("source") {
			source = c.get("src")
		}
	}
	return []*mNode{{typ: "link", title: n.get("title"), url: source, children: nodes}}
}

func (s *toMdast) input(n *hNode) []*mNode {
	switch n.get("type") {
	case "checkbox", "radio":
		if n.has("checked") {
			return []*mNode{mText("[x]")}
		}
		return []*mNode{mText("[ ]")}
	}
	return nil
}

func (s *toMdast) codeBlock(n *hNode) *mNode {
	lang := ""
	if n.tag == "pre" {
		for _, c := range n.children {
			if c.isEl("code") && len(c.classes) > 0 {
				for _, cls := range c.classes {
					if strings.HasPrefix(cls, "language-") {
						lang = strings.TrimPrefix(cls, "language-")
						break
					}
				}
				break
			}
		}
	}
	return &mNode{typ: "code", lang: lang, value: strings.TrimRight(toTextPre(n), "\r\n")}
}

// toTextPre is hast-util-to-text for a <pre>: text verbatim, <br> as a line
// feed.
func toTextPre(n *hNode) string {
	var sb strings.Builder
	var walk func(*hNode)
	walk = func(x *hNode) {
		for _, c := range x.children {
			switch {
			case c.kind == hText:
				sb.WriteString(c.value)
			case c.isEl("br"):
				sb.WriteString("\n")
			case c.kind == hElement:
				walk(c)
			}
		}
	}
	walk(n)
	return sb.String()
}

var bidiControls = regexp.MustCompile(`[\x{061C}\x{200E}\x{200F}\x{202A}-\x{202E}\x{2066}-\x{2069}]`)
var tabsOrSpaces = regexp.MustCompile(`[\t ]+`)

// toTextInline is hast-util-to-text for an inline <code> (normal whitespace):
// runs of spaces and tabs collapse to one, and a single space at either end
// survives.
func toTextInline(n *hNode) string {
	var sb strings.Builder
	var walk func(*hNode)
	walk = func(x *hNode) {
		for _, c := range x.children {
			switch {
			case c.kind == hText:
				sb.WriteString(collapseInlineText(c.value))
			case c.isEl("br"):
				sb.WriteString("\n")
			case c.kind == hElement:
				walk(c)
			}
		}
	}
	walk(n)
	return sb.String()
}

// collapseInlineText is collectText for one text node whose whitespace was
// already minified (so it holds no line feeds).
func collapseInlineText(value string) string {
	value = bidiControls.ReplaceAllString(value, "")
	return trimAndCollapseSpacesAndTabs(value, false, false)
}

func trimAndCollapseSpacesAndTabs(value string, breakBefore, breakAfter bool) string {
	var result []string
	start := 0
	end := 0
	for start < len(value) {
		loc := tabsOrSpaces.FindStringIndex(value[start:])
		var match []int
		if loc != nil {
			match = []int{start + loc[0], start + loc[1]}
		}
		if match != nil {
			end = match[0]
		} else {
			end = len(value)
		}
		if start == 0 && end == 0 && match != nil && !breakBefore {
			result = append(result, "")
		}
		if start != end {
			result = append(result, value[start:end])
		}
		if match != nil {
			start = match[1]
		} else {
			start = end
		}
	}
	if start != end && !breakAfter {
		result = append(result, "")
	}
	return strings.Join(result, " ")
}

func (s *toMdast) list(n *hNode) *mNode {
	ordered := n.tag == "ol"
	children := s.toSpecificContent(s.all(n), "listItem")
	start := 0
	if ordered {
		start = 1
		if v := n.get("start"); v != "" {
			if parsed, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && parsed != 0 {
				start = parsed
			}
		}
	}
	return &mNode{typ: "list", ordered: ordered, start: start, spread: listItemsSpread(children), children: children}
}

func listItemsSpread(children []*mNode) bool {
	if len(children) > 1 {
		for _, c := range children {
			if c.spread {
				return true
			}
		}
	}
	return false
}

func (s *toMdast) listItem(n *hNode) *mNode {
	rest, checkbox := extractLeadingCheckbox(n)
	var checked *bool
	if checkbox != nil {
		v := checkbox.has("checked")
		checked = &v
	}
	spread := spreadout(rest)
	return &mNode{typ: "listItem", spread: spread, checked: checked, children: s.toFlow(s.all(rest))}
}

func spreadout(n *hNode) bool {
	seenFlow := false
	for _, child := range n.children {
		if child.kind != hElement {
			continue
		}
		if hastPhrasing(child) {
			continue
		}
		if child.tag == "p" || seenFlow || spreadout(child) {
			return true
		}
		seenFlow = true
	}
	return false
}

func extractLeadingCheckbox(n *hNode) (*hNode, *hNode) {
	if len(n.children) == 0 {
		return n, nil
	}
	head := n.children[0]
	if head.isEl("input") && (head.get("type") == "checkbox" || head.get("type") == "radio") {
		rest := *n
		rest.children = n.children[1:]
		return &rest, head
	}
	if head.isEl("p") {
		restHead, checkbox := extractLeadingCheckbox(head)
		if checkbox != nil {
			rest := *n
			rest.children = append([]*hNode{restHead}, n.children[1:]...)
			return &rest, checkbox
		}
	}
	return n, nil
}

// ---- tables ----

func (s *toMdast) tableCell(n *hNode) *mNode {
	cell := &mNode{typ: "tableCell", children: s.all(n)}
	if n.has("rowSpan") || n.has("colSpan") {
		cell.spanInfo = &cellSpan{row: n.get("rowSpan"), col: n.get("colSpan")}
	}
	return cell
}

type cellSpan struct{ row, col string }

func (s *toMdast) table(n *hNode) *mNode {
	if s.inTable {
		return mText(toTextPre(n))
	}
	s.inTable = true
	align, headless := inspectTable(n)
	rows := s.toSpecificContent(s.all(n), "tableRow")
	if headless {
		rows = append([]*mNode{{typ: "tableRow"}}, rows...)
	}
	for _, row := range rows {
		row.children = s.toSpecificContent(row.children, "tableCell")
	}

	columns := 1
	for rowIndex, row := range rows {
		for cellIndex := 0; cellIndex < len(row.children); cellIndex++ {
			cell := row.children[cellIndex]
			if cell.spanInfo == nil {
				continue
			}
			colSpan := atoiOr(cell.spanInfo.col, 1)
			rowSpan := atoiOr(cell.spanInfo.row, 1)
			cell.spanInfo = nil
			if colSpan <= 1 && rowSpan <= 1 {
				continue
			}
			for other := rowIndex; other < rowIndex+rowSpan; other++ {
				for col := cellIndex; col < cellIndex+colSpan; col++ {
					if other >= len(rows) {
						break
					}
					// Cells a span covers get empty placeholders (the spanning
					// cell itself stays where it is).
					if other == rowIndex && col == cellIndex {
						continue
					}
					oc := rows[other].children
					at := col
					if at > len(oc) {
						at = len(oc)
					}
					merged := make([]*mNode, 0, len(oc)+1)
					merged = append(merged, oc[:at]...)
					merged = append(merged, &mNode{typ: "tableCell"})
					merged = append(merged, oc[at:]...)
					rows[other].children = merged
				}
			}
		}
		if len(row.children) > columns {
			columns = len(row.children)
		}
	}
	for _, row := range rows {
		for len(row.children) < columns {
			row.children = append(row.children, &mNode{typ: "tableCell"})
		}
	}
	for len(align) < columns {
		align = append(align, "")
	}
	s.inTable = false
	return &mNode{typ: "table", align: align, children: rows}
}

func atoiOr(s string, def int) int {
	if v, err := strconv.Atoi(strings.TrimSpace(s)); err == nil && v != 0 {
		return v
	}
	return def
}

// inspectTable finds the column alignments and whether the table lacks a
// header row (no <th> in its first row and no <thead>).
func inspectTable(table *hNode) ([]string, bool) {
	align := []string{""}
	headless := true
	rowIndex, cellIndex := 0, 0
	var visit func(n *hNode)
	visit = func(n *hNode) {
		for _, child := range n.children {
			if child.kind != hElement {
				continue
			}
			if child.tag == "table" {
				continue
			}
			switch child.tag {
			case "th", "td":
				for len(align) <= cellIndex {
					align = append(align, "")
				}
				if align[cellIndex] == "" {
					if v := child.get("align"); v == "center" || v == "left" || v == "right" {
						align[cellIndex] = v
					}
				}
				if headless && rowIndex < 2 && child.tag == "th" {
					headless = false
				}
				cellIndex++
			case "thead":
				headless = false
			case "tr":
				rowIndex++
				cellIndex = 0
			}
			visit(child)
		}
	}
	visit(table)
	return align, headless
}

// ---- post-processing (index.js) ----

var firstNewline = regexp.MustCompile(`[\t ]*(\r?\n|\r)[\t ]*`)
var leadingBlank = regexp.MustCompile(`^[\t ]+`)
var trailingBlank = regexp.MustCompile(`[\t ]+$`)

// normalizeMdast is the visit pass at the end of toMdast: adjacent text nodes
// merge, a newline loses the blanks around it, the first/last text of a
// paragraph, heading or root loses its outer blanks, and empty text goes.
func normalizeMdast(parent *mNode) {
	i := 0
	for i < len(parent.children) {
		child := parent.children[i]
		next := i + 1
		if child.typ == "text" {
			if i > 0 && parent.children[i-1].typ == "text" {
				parent.children[i-1].value += child.value
				parent.children = append(parent.children[:i], parent.children[i+1:]...)
				next = i - 1
			} else {
				if m := firstNewline.FindStringSubmatchIndex(child.value); m != nil {
					child.value = child.value[:m[0]] + child.value[m[2]:m[3]] + child.value[m[1]:]
				}
				if parent.typ == "heading" || parent.typ == "paragraph" || parent.typ == "root" {
					if i == 0 {
						child.value = leadingBlank.ReplaceAllString(child.value, "")
					}
					if i == len(parent.children)-1 {
						child.value = trailingBlank.ReplaceAllString(child.value, "")
					}
				}
				if child.value == "" {
					parent.children = append(parent.children[:i], parent.children[i+1:]...)
					next = i
				}
			}
		} else {
			normalizeMdast(child)
		}
		i = next
	}
}

// htmlToMdast runs BlockNote's rehype plugins and then hast-util-to-mdast.
func htmlToMdast(root *hNode) *mNode {
	convertVideoToMarkdown(root)
	removeUnderlines(root)
	addSpacesToCheckboxes(root)

	minifyWhitespace(root)
	state := &toMdast{}
	result := state.one(root, nil)
	var mdast *mNode
	switch {
	case len(result) == 0:
		mdast = &mNode{typ: "root"}
	default:
		mdast = result[0]
	}
	// toMdast reaches the same fixed point from the tree's own root node.
	normalizeMdast(&mNode{typ: "wrapper", children: []*mNode{mdast}})
	return mdast
}

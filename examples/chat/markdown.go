package main

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// markdown is a minimal, line-based markdown → ANSI renderer with fence state.
// Table rows are buffered until the block ends, since aligning columns needs
// every row before any can be printed.
type markdown struct {
	inFence     bool
	tableRows   []string
	bufferedRow bool // the last render() call buffered a table row
}

// render renders one line. It returns "" only when the line was buffered as a
// table row; such rows are emitted as one aligned block when the table ends
// (see takeTable). Any other line, including a blank one, renders to text.
func (m *markdown) render(line string) string {
	m.bufferedRow = false
	trimmed := strings.TrimRight(line, " \t")
	if isFence(trimmed) {
		m.inFence = !m.inFence
		return m.withTable(dim + trimmed + reset)
	}
	if m.inFence {
		return cyan + line + reset
	}
	if isTableRow(trimmed) {
		m.tableRows = append(m.tableRows, trimmed)
		m.bufferedRow = true
		return ""
	}
	return m.withTable(m.renderBlock(trimmed, line))
}

// renderBlock renders one non-fence, non-table line: a heading, rule, quote,
// list item, or plain text.
func (m *markdown) renderBlock(trimmed, raw string) string {
	if _, text, ok := heading(trimmed); ok {
		return bold + blue + inline(text) + reset
	}
	if isRule(trimmed) {
		return dim + strings.Repeat("─", 40) + reset
	}
	if strings.HasPrefix(trimmed, "> ") {
		return dim + "│ " + reset + inline(trimmed[2:])
	}
	if indent, marker, content, ok := listItem(trimmed); ok {
		return indent + marker + " " + inline(content)
	}
	return inline(raw)
}

// isFence reports whether a line opens or closes a ``` or ~~~ code fence.
func isFence(trimmed string) bool {
	return strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~")
}

// withTable prepends any buffered table rows to a rendered line, so a table
// block flushes as soon as a non-row line arrives.
func (m *markdown) withTable(rendered string) string {
	table := m.takeTable()
	if table == "" {
		return rendered
	}
	if rendered == "" {
		return table
	}
	return table + "\n" + rendered
}

// takeTable returns the buffered table rows rendered as an aligned grid, or ""
// when nothing is buffered.
func (m *markdown) takeTable() string {
	if len(m.tableRows) == 0 {
		return ""
	}
	rows := m.tableRows
	m.tableRows = nil
	return renderTable(rows)
}

// isTableRow reports whether a line opens or continues a table block.
func isTableRow(line string) bool {
	return strings.HasPrefix(line, "|")
}

// renderTable renders table rows as an aligned grid with a bold header and dim
// borders. The second row must be a |---|---| separator for the block to count
// as a table; otherwise the lines render literally.
func renderTable(rows []string) string {
	cells := make([][]string, len(rows))
	for i, row := range rows {
		cells[i] = tableCells(row)
	}
	if len(cells) < 2 || !isTableSeparator(cells[1]) {
		return strings.Join(rows, "\n")
	}
	// Column widths come from the content rows; the |---| separator row would
	// otherwise hold short columns open.
	widths := columnWidths(slices.Concat(cells[:1], cells[2:]))
	out := []string{
		tableLine(cells[0], widths, bold),
		tableRule(widths),
	}
	for _, row := range cells[2:] {
		out = append(out, tableLine(row, widths, ""))
	}
	return strings.Join(out, "\n")
}

// tableCells splits one "| a | b |" row into its trimmed cells.
func tableCells(row string) []string {
	row = strings.TrimSpace(row)
	row = strings.TrimPrefix(row, "|")
	row = strings.TrimSuffix(row, "|")
	fields := strings.Split(row, "|")
	cells := make([]string, len(fields))
	for i, field := range fields {
		cells[i] = strings.TrimSpace(field)
	}
	return cells
}

// isTableSeparator reports whether cells look like ---, :--- or ---:, the
// second row of a markdown table. At least one dash is required, so a row of
// bare colons does not count.
func isTableSeparator(cells []string) bool {
	if len(cells) == 0 {
		return false
	}
	for _, cell := range cells {
		// Strip the surrounding colons, then the dashes: whatever remains is
		// a character this row may not contain.
		if rest := strings.Trim(strings.Trim(cell, ":"), "-"); rest != "" {
			return false
		}
		if !strings.Contains(cell, "-") {
			return false
		}
	}
	return true
}

// ansiStyleRE matches SGR escape sequences, which occupy no terminal columns.
var ansiStyleRE = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// visibleWidth is the terminal width of an inline-rendered string. The
// rendered text cannot be measured directly (styles add escapes and links
// append the dimmed URL), so the escapes are stripped first.
func visibleWidth(rendered string) int {
	return stringWidth(ansiStyleRE.ReplaceAllLiteralString(rendered, ""))
}

// columnWidths returns the widest display width of each column.
func columnWidths(rows [][]string) []int {
	var widths []int
	for _, row := range rows {
		for i, cell := range row {
			if i == len(widths) {
				widths = append(widths, 0)
			}
			if w := visibleWidth(inline(cell)); w > widths[i] {
				widths[i] = w
			}
		}
	}
	return widths
}

// tableLine renders one row: dim borders around space-padded cells. The header
// style (when set) wraps each cell, since resets inside the line would
// otherwise end the style early. Padding uses visibleWidth, so CJK cells and
// cells with inline spans align.
func tableLine(cells []string, widths []int, style string) string {
	border := dim + "│" + reset
	var out strings.Builder
	out.WriteString(border)
	for i, width := range widths {
		cell, pad := "", width
		if i < len(cells) {
			cell = inline(cells[i])
			pad = width - visibleWidth(cell)
			if style != "" {
				cell = style + cell + reset
			}
		}
		out.WriteString(" " + cell + strings.Repeat(" ", pad) + " ")
		out.WriteString(border)
	}
	return out.String()
}

// tableRule renders the line under the header: one ─ run per column.
func tableRule(widths []int) string {
	parts := make([]string, len(widths))
	for i, width := range widths {
		parts[i] = strings.Repeat("─", width+2)
	}
	return dim + "├" + strings.Join(parts, "┼") + "┤" + reset
}

// listItem splits a list line into its indentation, marker and content, so
// nested items keep their indentation and ordered items keep their number (the
// model emits both).
func listItem(line string) (indent, marker, content string, ok bool) {
	body := strings.TrimLeft(line, " \t")
	indent = line[:len(line)-len(body)]
	if len(body) >= 2 {
		switch body[0] {
		case '-', '*', '+':
			if body[1] == ' ' {
				return indent, "•", body[2:], true
			}
		}
	}
	if end := strings.IndexAny(body, ".)"); end > 0 && end <= 3 && end+1 < len(body) && body[end+1] == ' ' {
		if _, err := strconv.Atoi(body[:end]); err == nil {
			return indent, body[:end+1], body[end+2:], true
		}
	}
	return "", "", "", false
}

func heading(line string) (int, string, bool) {
	if !strings.HasPrefix(line, "#") {
		return 0, "", false
	}
	level := 0
	for level < len(line) && line[level] == '#' {
		level++
	}
	if level > 6 || level >= len(line) || line[level] != ' ' {
		return 0, "", false
	}
	return level, strings.TrimSpace(line[level:]), true
}

func isRule(line string) bool {
	if len(line) < 3 {
		return false
	}
	c := line[0]
	if c != '-' && c != '*' && c != '_' {
		return false
	}
	for i := 0; i < len(line); i++ {
		if line[i] != c {
			return false
		}
	}
	return true
}

// inline renders the common inline spans: code, bold, links, italic.
func inline(s string) string {
	var out strings.Builder
	for i := 0; i < len(s); {
		span, size, ok := inlineSpan(s[i:])
		if !ok {
			out.WriteByte(s[i])
			i++
			continue
		}
		out.WriteString(span)
		i += size
	}
	return out.String()
}

// inlineSpan matches one inline span at the start of s, returning its rendering
// and its length in bytes.
func inlineSpan(s string) (string, int, bool) {
	switch {
	case s[0] == '`':
		return codeSpan(s)
	case strings.HasPrefix(s, "**"):
		return boldSpan(s)
	case s[0] == '[':
		return linkSpan(s)
	case s[0] == '*':
		return italicSpan(s)
	}
	return "", 0, false
}

// codeSpan renders `code`.
func codeSpan(s string) (string, int, bool) {
	end := strings.IndexByte(s[1:], '`')
	if end < 0 {
		return "", 0, false
	}
	return cyan + s[1:1+end] + reset, end + 2, true
}

// boldSpan renders **bold**, recursing so nested spans still render.
func boldSpan(s string) (string, int, bool) {
	end := strings.Index(s[2:], "**")
	if end < 0 {
		return "", 0, false
	}
	return bold + inline(s[2:2+end]) + reset, end + 4, true
}

// linkSpan renders [text](url) as the underlined text followed by the dimmed URL.
func linkSpan(s string) (string, int, bool) {
	close := strings.IndexByte(s, ']')
	if close < 0 || close+1 >= len(s) || s[close+1] != '(' {
		return "", 0, false
	}
	urlEnd := strings.IndexByte(s[close+2:], ')')
	if urlEnd < 0 {
		return "", 0, false
	}
	text, url := s[1:close], s[close+2:close+2+urlEnd]
	return underline + inline(text) + reset + dim + " (" + url + ")" + reset, close + 2 + urlEnd + 1, true
}

// italicSpan renders *italic*. Emphasis cannot start or end with a space, so
// arithmetic like "2 * 3 * 4" stays literal instead of rendering as italic.
func italicSpan(s string) (string, int, bool) {
	end := strings.IndexByte(s[1:], '*')
	if end <= 0 {
		return "", 0, false
	}
	text := s[1 : 1+end]
	if text != strings.TrimSpace(text) {
		return "", 0, false
	}
	return italic + inline(text) + reset, end + 2, true
}

// markdownStream renders markdown line by line as text streams in.
type markdownStream struct {
	md    markdown
	buf   strings.Builder
	write func(string)
}

func newMarkdownStream(write func(string)) *markdownStream {
	return &markdownStream{write: write}
}

func (s *markdownStream) Write(delta string) {
	for _, r := range delta {
		if r == '\n' {
			// A buffered table row renders to "" and must not emit a blank
			// line; anything else (including a genuinely blank line) writes.
			if out := s.md.render(s.buf.String()); out != "" || !s.md.bufferedRow {
				s.write(out + "\n")
			}
			s.buf.Reset()
			continue
		}
		s.buf.WriteRune(r)
	}
}

func (s *markdownStream) Flush() {
	if s.buf.Len() > 0 {
		s.write(s.md.render(s.buf.String()))
		s.buf.Reset()
	}
	if table := s.md.takeTable(); table != "" {
		s.write(table + "\n")
	}
}

// renderMarkdown renders a complete markdown block (used for /task output).
func renderMarkdown(text string) string {
	md := &markdown{}
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	for i, line := range lines {
		lines[i] = md.render(line)
	}
	if table := md.takeTable(); table != "" {
		lines = append(lines, table)
	}
	return strings.Join(lines, "\n") + "\n"
}

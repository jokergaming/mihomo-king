package cli

import (
	"bytes"
	"io"
	"strings"

	"github.com/mattn/go-runewidth"
)

// table buffers tab-separated rows and aligns the columns by display width on
// Flush. Unlike text/tabwriter it measures CJK characters and emoji (common in
// node names) as two cells, so the columns line up in a terminal.
type table struct {
	out io.Writer
	buf bytes.Buffer
}

func newTable(out io.Writer) *table { return &table{out: out} }

func (t *table) Write(p []byte) (int, error) { return t.buf.Write(p) }

func (t *table) Flush() error {
	var rows [][]string
	var widths []int
	for line := range strings.Lines(t.buf.String()) {
		cells := strings.Split(strings.TrimSuffix(line, "\n"), "\t")
		for i, c := range cells[:len(cells)-1] { // the last column is not padded
			if i == len(widths) {
				widths = append(widths, 0)
			}
			widths[i] = max(widths[i], runewidth.StringWidth(c))
		}
		rows = append(rows, cells)
	}
	var b strings.Builder
	for _, cells := range rows {
		for i, c := range cells {
			b.WriteString(c)
			if i < len(cells)-1 {
				b.WriteString(strings.Repeat(" ", widths[i]-runewidth.StringWidth(c)+2))
			}
		}
		b.WriteString("\n")
	}
	t.buf.Reset()
	_, err := io.WriteString(t.out, b.String())
	return err
}

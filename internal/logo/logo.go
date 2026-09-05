// Package logo renders the pigeon dove/wordmark artwork (assets/pigeon.svg)
// as half-block Unicode art for terminal display. It has no dependencies
// beyond lipgloss, so both internal/tui (the help panel) and internal/cli
// (the --help banner) can depend on it directly without either frontend
// depending on the other.
package logo

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// white and gray match the block colors in assets/pigeon.svg.
var (
	white = lipgloss.Color("#dcdee3")
	gray  = lipgloss.Color("#a3a6b1")
)

// doveGrid is the dove artwork from assets/pigeon.svg downsampled to its
// native 18x18 block grid: 'W' white, 'G' gray, '.' transparent. Keep this in
// sync by hand if that artwork changes.
var doveGrid = []string{
	".......G..........",
	".......W..........",
	"WW....GWG.........",
	".GWW..GGWG....WW..",
	".WGWWWWGGG...GWGW.",
	"..WWGWWWWGG..WWWWG",
	"...GWWWWWWG.GWWW..",
	"...GGGWWWWW.WWWW..",
	"...WGGWWWWWGWWWW..",
	"...GWWWWWWWWWWWW..",
	".....GGWWGWWWWWW..",
	".......GGWWWWWWG..",
	".......WWWWWWWG...",
	"......GWWWWWWG....",
	"...GGWWWGGGG......",
	".GWWWGW...........",
	"...WGW............",
	"....G.............",
}

// titleGrid is the "PIGEON" wordmark from assets/pigeon.svg, sampled
// (point-in-rect against the SVG's letter blocks) onto a 48x8 grid: 'W'
// white, '.' transparent. Keep this in sync by hand if that artwork changes.
var titleGrid = []string{
	"WWWWWWW.WWWWW..WWWWWW..WWWWWW..WWWWWW..WWW...WWW",
	"WWWWWWW.WWWWW..WWWWWW..WWWWWW..WWWWWW..WWW...WWW",
	"WW...WWW.WWW..WW.......WWWW...WW...WWW.WW.W..WWW",
	"WW...WWW.WWW..WW.......WWWW...WW...WWW.WW.W..WWW",
	"WWWWWWW..WWW..WW...WWW.WWWW...WW...WWW.WW..WWWWW",
	"WWWWWWW..WWW..WW...WWW.WWWW...WW...WWW.WW..WWWWW",
	"WW......WWWWW..WWWWWW..WWWWWW..WWWWWW..WW...WWWW",
	"WW......WWWWW..WWWWWW..WWWWWW..WWWWWW..WW...WWWW",
}

// dove renders doveGrid as half-block Unicode art.
func dove() []string {
	return renderBlockGrid(doveGrid)
}

// title renders titleGrid (the "PIGEON" wordmark) as half-block Unicode art,
// and barLine renders the search-bar strip beneath it at the same column
// width, so the two stack into one banner in Banner.
func title() []string {
	return renderBlockGrid(titleGrid)
}

func barLine() string {
	bar := strings.Repeat("█", len(titleGrid[0]))
	return lipgloss.NewStyle().Foreground(white).Render(bar)
}

// Banner joins the dove, wordmark, and bar into the full pigeon logo: the
// dove on the left, the wordmark and bar stacked on the right, vertically
// centered against the (taller) dove.
func Banner() string {
	doveBlock := strings.Join(dove(), "\n")
	right := strings.Join(append(title(), "", barLine()), "\n")
	return lipgloss.JoinHorizontal(lipgloss.Center, doveBlock, "  ", right)
}

// renderBlockGrid turns a grid of 'W'/'G'/'.' rows into half-block Unicode
// art (▀/▄), packing two pixel rows into one terminal row (via top/bottom
// half-block glyphs) so the art reads at roughly its true aspect ratio in a
// terminal, where a character cell is about twice as tall as it is wide.
func renderBlockGrid(grid []string) []string {
	lines := make([]string, 0, (len(grid)+1)/2)
	for r := 0; r < len(grid); r += 2 {
		top := grid[r]
		bottom := ""
		if r+1 < len(grid) {
			bottom = grid[r+1]
		}
		var line strings.Builder
		for c := 0; c < len(top); c++ {
			bo := byte('.')
			if c < len(bottom) {
				bo = bottom[c]
			}
			line.WriteString(cell(top[c], bo))
		}
		lines = append(lines, line.String())
	}
	return lines
}

// cell renders one terminal cell from a pair of vertically stacked pixels
// ('W'/'G'/'.'), choosing the upper/lower half-block glyph (or a blank) so
// both pixels' colors show even though they share one cell.
func cell(top, bottom byte) string {
	style := lipgloss.NewStyle()
	switch {
	case top == '.' && bottom == '.':
		return " "
	case top != '.' && bottom == '.':
		return style.Foreground(color(top)).Render("▀")
	case top == '.' && bottom != '.':
		return style.Foreground(color(bottom)).Render("▄")
	default:
		return style.Foreground(color(top)).Background(color(bottom)).Render("▀")
	}
}

func color(cls byte) lipgloss.Color {
	if cls == 'G' {
		return gray
	}
	return white
}

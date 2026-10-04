// Package render lays out a parsed note (internal/md) as the reader's page
// described in DESIGN.md §5, and serialises pages as ANSI or plain text.
//
// # Model
//
//	page := render.Render(doc, render.Options{Width: 100, Theme: th, Glyphs: g, ...})
//
// A Page is a slice of Lines in final visual order: each Line is a run of
// styled Spans laid out from column 0 of the pane, margins, centring,
// indentation, bidi reordering and Arabic shaping already applied, with an
// exact cell Width that never exceeds Options.Width. Every line records
// where it came from (SrcStart/SrcEnd source lines, Kind, Obsidian BlockID,
// heading Level), whether it carries a fold chevron (Fold, Folded), its
// interactive regions (Hits: links, task boxes, chevrons, footnote marks)
// and, for code lines, a CodeView for horizontal scrolling (Line.Scrolled).
//
// The Page also lists the links in document order with every region they
// occupy (Tab navigation, Page.FocusSpans for the highlighted link), the
// outline, the foldable elements and the footnote definitions, and maps
// source lines to rendered lines and back (LineForSource, SourceLine) so
// switching between reader and editor keeps the place.
//
// # Layout
//
// The measure is min(Options.Measure (78), Width − 2·Margin), centred in
// the pane; the left margin holds the reader's cursor (Page.CursorX) and the
// fold chevrons (Page.ChevronX). A table may grow past the measure into the
// pane before its columns shrink (ColumnWidths), and falls back to stacked
// records when a column would become too narrow.
//
// Folds are keyed by id: "h:<slug>" for top-level headings, "c:<n>" for
// the n-th foldable callout (1-based, document order) and "properties" for
// the frontmatter properties line. Options.Folds overrides the defaults
// (headings open, callouts as written, properties folded); Page.Folds lists
// what a page contains, including folds hidden inside folded sections.
//
// # Links and embeds
//
// The renderer does not know the vault. A Resolver reports whether an
// internal link resolves (and to which path) and supplies the source of
// embedded notes. Same-note anchors ([[#Heading]]) are resolved against the
// document itself.
//
// # Colour and terminals
//
// Styles are written in the theme's colours. Pass the theme variant for the
// output profile (term.ThemeFor): with Basic16 and Mono themes (no Raised
// colour) code blocks are framed by a faint bar and inline code keeps its
// backticks. Page.ANSI serialises with a term.Encoder (which downsamples
// and emits OSC 8 hyperlinks for external links when enabled); Page.Plain
// returns text without escapes. Render with Bidi off for pipes: plain output
// is never reordered or shaped. All note text is sanitised: control
// characters (ESC included) and explicit bidi controls never reach the
// terminal.
package render

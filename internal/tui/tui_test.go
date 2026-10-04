package tui

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ZahakJ/astrolabe-cli/internal/cli"
	"github.com/ZahakJ/astrolabe-cli/internal/editor"
	"github.com/ZahakJ/astrolabe-cli/internal/render"
	"github.com/ZahakJ/astrolabe-cli/internal/term"
	"github.com/ZahakJ/astrolabe-cli/internal/theme"
	"github.com/ZahakJ/astrolabe-cli/internal/vault"
)

func TestOpenNotePaintsPageAndStatus(t *testing.T) {
	h := newHarness(t, harnessOpt{}).open("Home.md")
	h.contains("Home", "Now", "Reference", "The front door of this vault")
	st := h.statusLine()
	for _, want := range []string{"✦", "Home", "words", "min", "READ"} {
		if !strings.Contains(st, want) {
			t.Fatalf("status bar %q lacks %q", st, want)
		}
	}
	// The cursor bar sits in the gutter of the cursor line.
	d := h.a.doc
	if c := h.vt.Cell(d.page.CursorX, 1+d.cur-d.top); c.Text != "▎" {
		t.Fatalf("cursor bar = %q, want ▎", c.Text)
	}
}

func TestCursorSkipsBlankLinesAndCounts(t *testing.T) {
	h := newHarness(t, harnessOpt{}).open("Home.md")
	h.keys("j")
	if h.cursorText() == "" {
		t.Fatal("cursor landed on a blank line")
	}
	first := h.a.doc.cur
	h.keys("3 j")
	if h.a.doc.cur <= first {
		t.Fatal("3j did not move")
	}
	h.keys("G")
	last := h.a.doc.cur
	h.keys("g g")
	if h.a.doc.cur >= last || h.a.doc.cur != h.a.snapCursor(h.a.doc, 0, 1) {
		t.Fatalf("gg went to %d", h.a.doc.cur)
	}
}

func TestFollowLinkAndHistory(t *testing.T) {
	h := newHarness(t, harnessOpt{}).open("Home.md")
	h.keys("tab")
	if h.a.doc.focus < 0 {
		t.Fatal("Tab highlighted no link")
	}
	from := h.cursorText()
	h.keys("enter")
	if h.a.doc.path != "projects/Lantern migration.md" {
		t.Fatalf("followed to %q", h.a.doc.path)
	}
	h.contains("Move every background job")
	h.keys("backspace")
	if h.a.doc.path != "Home.md" {
		t.Fatalf("back went to %q", h.a.doc.path)
	}
	if got := h.cursorText(); got != from {
		t.Fatalf("cursor not restored: %q, want %q", got, from)
	}
	h.keys("L")
	if h.a.doc.path != "projects/Lantern migration.md" {
		t.Fatalf("forward went to %q", h.a.doc.path)
	}
	h.keys("H")
	if h.a.doc.path != "Home.md" {
		t.Fatalf("H went to %q", h.a.doc.path)
	}
}

func TestFollowHeadingAnchor(t *testing.T) {
	h := newHarness(t, harnessOpt{}).open("areas/On-call.md")
	h.gotoText("Re-read")
	h.keys("enter")
	if h.a.doc.path != "runbooks/Lantern cutover.md" {
		t.Fatalf("followed to %q", h.a.doc.path)
	}
	if got := h.cursorText(); !strings.Contains(got, "Rollback") {
		t.Fatalf("cursor on %q, want the Rollback heading", got)
	}
}

func TestBrokenLinkOffersCreation(t *testing.T) {
	h := newHarness(t, harnessOpt{}).open("Typography.md")
	// Tab to the broken link.
	d := h.a.doc
	idx := -1
	for i, l := range d.page.Links {
		if l.Status == render.LinkBroken && l.Target == "Missing note" {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatal("no broken link in Typography.md")
	}
	d.focus = idx
	d.cur = d.page.Links[idx].Line
	h.keys("enter")
	h.contains("Create “Missing note”?")
	h.keys("y")
	if _, err := os.Stat(filepath.Join(h.dir, "Missing note.md")); err != nil {
		t.Fatalf("note not created: %v", err)
	}
	if h.a.view != viewEditor {
		t.Fatal("new note did not open in the editor")
	}
}

// A broken link to an attachment says it is missing; it never offers to
// create "x.png.md".
func TestBrokenAttachmentDoesNotOfferCreation(t *testing.T) {
	dir := copyVault(t)
	os.WriteFile(filepath.Join(dir, "Att.md"), []byte("# Att\n\n![[ghost.png]]\n\nSee [[minutes.pdf]] and [[Release 1.2]].\n"), 0o644)
	h := newHarness(t, harnessOpt{dir: dir}).open("Att.md")
	follow := func(target string) {
		t.Helper()
		d := h.a.doc
		idx := -1
		for i, l := range d.page.Links {
			if l.Target == target {
				idx = i
			}
		}
		if idx < 0 {
			t.Fatalf("no link to %q", target)
		}
		if d.page.Links[idx].Status != render.LinkBroken {
			t.Fatalf("%q not broken", target)
		}
		d.focus = idx
		d.cur = d.page.Links[idx].Line
		h.keys("enter")
	}
	for _, target := range []string{"ghost.png", "minutes.pdf"} {
		follow(target)
		if len(h.a.overlays) != 0 {
			t.Fatalf("%s: an overlay opened:\n%s", target, h.screen())
		}
		if !strings.Contains(h.statusLine(), "missing attachment: "+target) {
			t.Fatalf("%s: status %q", target, h.statusLine())
		}
		if _, err := os.Stat(filepath.Join(dir, target+".md")); err == nil {
			t.Fatalf("%s.md was created", target)
		}
	}
	// A note name with a dot is still a note.
	follow("Release 1.2")
	h.contains("Create “Release 1.2”?")
	h.keys("esc")
}

func TestIsAttachment(t *testing.T) {
	for in, want := range map[string]bool{
		"x.png": true, "a/b/report.PDF": true, "data.tar.gz": true, "clip.mp4": true,
		"Note": false, "Note.md": false, "Release 1.2": false, "Mr. Smith": false,
		"2026.10.04": false, ".hidden": false, "trailing.": false,
	} {
		if got := isAttachment(in); got != want {
			t.Errorf("isAttachment(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestExternalLinkIsCopiedNotOpened(t *testing.T) {
	h := newHarness(t, harnessOpt{}).open("Inbox.md")
	h.gotoText("fair-queues")
	h.keys("enter")
	if len(h.host.copied) != 1 || !strings.Contains(h.host.copied[0], "example.org/posts/fair-queues") {
		t.Fatalf("copied %q", h.host.copied)
	}
	if !strings.Contains(h.statusLine(), "copied") {
		t.Fatalf("status %q", h.statusLine())
	}
	if len(h.host.ran) != 0 {
		t.Fatal("an external program was run for a URL")
	}
}

func TestFoldSection(t *testing.T) {
	h := newHarness(t, harnessOpt{}).open("Home.md")
	h.gotoText("Reference")
	h.keys("z a")
	h.contains("▸ Reference").lacks("Operations")
	if !strings.Contains(h.cursorText(), "Reference") {
		t.Fatalf("cursor left the folded heading: %q", h.cursorText())
	}
	h.keys("z o")
	h.contains("Operations")
	h.keys("z M")
	h.lacks("Operations", "Lately I have been")
	h.keys("z R")
	h.contains("Operations", "Lately I have been")
}

func TestToggleTaskWritesOneByte(t *testing.T) {
	h := newHarness(t, harnessOpt{}).open("areas/On-call.md")
	before := h.read("areas/On-call.md")
	h.gotoText("Test the pager")
	h.keys("x")
	after := h.read("areas/On-call.md")
	if len(before) != len(after) {
		t.Fatalf("length changed: %d → %d", len(before), len(after))
	}
	diff := 0
	for i := range before {
		if before[i] != after[i] {
			diff++
			if after[i] != 'x' {
				t.Fatalf("byte %d became %q", i, after[i])
			}
		}
	}
	if diff != 1 {
		t.Fatalf("%d bytes changed, want 1", diff)
	}
	h.contains("☑")
	if !strings.Contains(h.cursorText(), "Test the pager") {
		t.Fatalf("cursor moved to %q", h.cursorText())
	}
	h.keys("x")
	if h.read("areas/On-call.md") != before {
		t.Fatal("second toggle did not restore the file")
	}
}

func TestFinderOpensNote(t *testing.T) {
	h := newHarness(t, harnessOpt{}).open("Home.md")
	h.keys("ctrl+p")
	h.contains("Find note")
	h.typ("tidewat")
	h.contains("Tidewater")
	h.keys("enter")
	if h.a.doc.path != "projects/Tidewater.md" {
		t.Fatalf("opened %q", h.a.doc.path)
	}
	h.lacks("Find note")
	// Back returns to Home.
	h.keys("backspace")
	if h.a.doc.path != "Home.md" {
		t.Fatalf("back to %q", h.a.doc.path)
	}
}

func TestFinderEmptyQueryListsRecentFirst(t *testing.T) {
	h := newHarness(t, harnessOpt{}).open("Home.md")
	h.a.openNote("Inbox.md", 0)
	h.a.openNote("projects/Tidewater.md", 0)
	h.frame()
	h.keys("space f")
	f := h.a.overlays[len(h.a.overlays)-1].(*finder)
	if got := f.selected().Path; got != "Inbox.md" {
		t.Fatalf("first recent = %q, want the previous note Inbox.md", got)
	}
}

func TestCaptureAppendsOneLine(t *testing.T) {
	h := newHarness(t, harnessOpt{}).open("Home.md")
	daily := filepath.Join(h.dir, "daily", "2026-10-04.md")
	h.keys("space c")
	h.contains("Capture")
	h.typ("call the printer")
	h.keys("enter")
	b, err := os.ReadFile(daily)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(b), "- 10:00 call the printer\n") {
		t.Fatalf("daily note:\n%s", b)
	}
	if h.a.doc.path != "Home.md" {
		t.Fatal("capture left the page")
	}
	if !strings.Contains(h.statusLine(), "captured to daily/2026-10-04.md") {
		t.Fatalf("status %q", h.statusLine())
	}
	// A task capture with Tab.
	h.keys("space c")
	h.typ("water the plants")
	h.keys("tab enter")
	b, _ = os.ReadFile(daily)
	if !strings.HasSuffix(string(b), "- 10:00 call the printer\n- [ ] water the plants\n") {
		t.Fatalf("daily note:\n%s", b)
	}
}

func TestEditSaveRoundTrip(t *testing.T) {
	h := newHarness(t, harnessOpt{}).open("Home.md")
	h.gotoText("The front door")
	h.keys("e")
	if h.a.view != viewEditor {
		t.Fatal("e did not open the editor")
	}
	if !strings.Contains(h.statusLine(), "NORMAL") {
		t.Fatalf("status %q", h.statusLine())
	}
	if h.a.edit.ed.CursorLine() != 6 {
		t.Fatalf("editor opened on line %d, want 6 (after the frontmatter)", h.a.edit.ed.CursorLine())
	}
	h.keys("A")
	if !strings.Contains(h.statusLine(), "INSERT") {
		t.Fatalf("status %q", h.statusLine())
	}
	h.typ(" Welcome in.")
	h.keys("esc")
	if !strings.Contains(h.statusLine(), "●") {
		t.Fatalf("no dirty mark: %q", h.statusLine())
	}
	h.keys("esc")
	if h.a.view != viewReader {
		t.Fatal("Esc Esc did not return to the reader")
	}
	if !strings.Contains(h.read("Home.md"), "one link away. Welcome in.\n") {
		t.Fatalf("not saved:\n%s", h.read("Home.md"))
	}
	if !strings.Contains(h.cursorText(), "Welcome in.") {
		t.Fatalf("reader not back on the block: %q", h.cursorText())
	}
	h.contains("Welcome in.")
	// The index sees the edit.
	if n, ok := h.a.v.Note("Home.md"); !ok || n.Size != int64(len(h.read("Home.md"))) {
		t.Fatal("note not re-indexed after save")
	}
}

// The reader's i and a open the editor already in Insert mode (DESIGN.md
// §4.3: the loop is i … type … Esc Esc): i where the line's text starts,
// after any list/task/quote/heading marker; a at the end of the line.
func TestEditInsertEntry(t *testing.T) {
	h := newHarness(t, harnessOpt{}).open("Home.md")
	h.gotoText("The front door")
	h.keys("i")
	if h.a.view != viewEditor || !strings.Contains(h.statusLine(), "INSERT") {
		t.Fatalf("i: view %v, status %q", h.a.view, h.statusLine())
	}
	h.typ("Hello. ")
	h.keys("esc esc")
	if h.a.view != viewReader {
		t.Fatal("i … Esc Esc did not return to the reader")
	}
	if !strings.Contains(h.read("Home.md"), "\nHello. The front door of this vault.") {
		t.Fatalf("i inserted in the wrong place:\n%s", h.read("Home.md"))
	}
	// On a list item, i types after the marker.
	h.gotoText("search indexing")
	h.keys("i")
	h.typ("NB ")
	h.keys("esc esc")
	if !strings.Contains(h.read("Home.md"), "\n- NB [[Tidewater]]") {
		t.Fatalf("i on a list item:\n%s", h.read("Home.md"))
	}
	// On a heading, after the #s.
	h.gotoText("Reference")
	h.keys("i")
	h.typ("Quick ")
	h.keys("esc esc")
	if !strings.Contains(h.read("Home.md"), "\n## Quick Reference\n") {
		t.Fatalf("i on a heading:\n%s", h.read("Home.md"))
	}
	// a appends to the end of the source line.
	h.gotoText("The front door")
	h.keys("a")
	if !strings.Contains(h.statusLine(), "INSERT") {
		t.Fatalf("a: status %q", h.statusLine())
	}
	h.typ(" Come in.")
	h.keys("esc esc")
	if !strings.Contains(h.read("Home.md"), "one link away. Come in.\n") {
		t.Fatalf("a appended in the wrong place:\n%s", h.read("Home.md"))
	}
	// Opening and leaving without typing rewrites nothing.
	before := h.read("Home.md")
	h.keys("i esc esc")
	if h.a.view != viewReader || h.read("Home.md") != before {
		t.Fatal("i Esc Esc without typing changed the file or stayed in the editor")
	}
	// One undo step: u in the editor removes the whole insert.
	h.gotoText("Hello.")
	h.keys("a")
	h.typ(" Twice.")
	h.keys("esc u esc")
	if h.read("Home.md") != before {
		t.Fatal("u did not undo the a insert as one step")
	}
}

// A termination signal mid-edit saves the buffer: to the note when it is
// unchanged on disk, otherwise to the recovered directory, which the next
// start announces once.
func TestSignalSavesDirtyBuffer(t *testing.T) {
	h := newHarness(t, harnessOpt{}).open("Inbox.md")
	h.keys("i")
	h.typ("kept through a hangup ")
	sig := make(chan chan struct{})
	h.a.sigReq = sig
	done := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := make(chan term.Event)
	go func() {
		h.a.loop(ctx, events, func() error { return nil })
		h.a.finish()
	}()
	sig <- done
	<-done
	if !strings.Contains(h.read("Inbox.md"), "kept through a hangup ") {
		t.Fatalf("note not saved:\n%s", h.read("Inbox.md"))
	}
	if fs := vault.RecoveredFiles(filepath.Join(h.state, "recovered")); len(fs) != 0 {
		t.Fatalf("recovered written although the note was safe: %v", fs)
	}

	// The file changed on disk meanwhile: the buffer goes to recovered.
	h2 := newHarness(t, harnessOpt{}).open("Inbox.md")
	h2.keys("i")
	h2.typ("mine ")
	time.Sleep(10 * time.Millisecond)
	os.WriteFile(filepath.Join(h2.dir, "Inbox.md"), []byte("# Inbox\n\ntheirs\n"), 0o644)
	h2.a.emergencySave()
	if got := h2.read("Inbox.md"); got != "# Inbox\n\ntheirs\n" {
		t.Fatalf("clobbered a changed file:\n%s", got)
	}
	rdir := filepath.Join(h2.state, "recovered")
	fs := vault.RecoveredFiles(rdir)
	if len(fs) != 1 || !strings.HasSuffix(fs[0], "-Inbox.md") {
		t.Fatalf("recovered %v", fs)
	}
	if b, _ := os.ReadFile(fs[0]); !strings.Contains(string(b), "mine ") {
		t.Fatalf("recovered content:\n%s", b)
	}
	// Leaving any other way than a confirmed quit keeps the edits too.
	h2.a.finish()
	if fs := vault.RecoveredFiles(rdir); len(fs) != 1 {
		t.Fatalf("finish wrote the buffer again: %v", fs)
	}
	// The next start says so, once.
	h2.a.announceRecovered()
	if !strings.Contains(h2.a.msg.text, "recovered to "+fs[0]) {
		t.Fatalf("message %q", h2.a.msg.text)
	}
	h2.a.msg = message{}
	h2.a.announceRecovered()
	if h2.a.msg.text != "" {
		t.Fatalf("announced twice: %q", h2.a.msg.text)
	}

	// A confirmed "quit anyway" drops them.
	h3 := newHarness(t, harnessOpt{}).open("Inbox.md")
	before := h3.read("Inbox.md")
	h3.keys("i")
	h3.typ("dropped")
	h3.keys("esc")
	h3.a.requestQuit()
	h3.keys("y")
	h3.a.finish()
	if h3.read("Inbox.md") != before || len(vault.RecoveredFiles(filepath.Join(h3.state, "recovered"))) != 0 {
		t.Fatal("confirmed quit kept the edits")
	}
}

func TestEditConflictOverwriteAndCancel(t *testing.T) {
	h := newHarness(t, harnessOpt{}).open("Inbox.md")
	h.keys("e")
	path := filepath.Join(h.dir, "Inbox.md")
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(path, []byte("# Inbox\n\nchanged elsewhere\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.keys("o")
	h.typ("mine")
	h.keys("esc esc")
	h.contains("Changed on disk", "reload theirs", "overwrite with mine")
	h.keys("c")
	if h.a.view != viewEditor {
		t.Fatal("cancel left the editor")
	}
	if !strings.Contains(h.read("Inbox.md"), "changed elsewhere") {
		t.Fatal("cancel wrote the file")
	}
	h.keys("esc")
	h.contains("Changed on disk")
	h.keys("o")
	if h.a.view != viewReader {
		t.Fatal("overwrite did not leave the editor")
	}
	if got := h.read("Inbox.md"); !strings.Contains(got, "mine") || strings.Contains(got, "changed elsewhere") {
		t.Fatalf("overwrite wrote:\n%s", got)
	}
}

func TestEditConflictReload(t *testing.T) {
	h := newHarness(t, harnessOpt{}).open("Inbox.md")
	h.keys("e")
	time.Sleep(10 * time.Millisecond)
	os.WriteFile(filepath.Join(h.dir, "Inbox.md"), []byte("# Inbox\n\ntheirs\n"), 0o644)
	h.keys("o")
	h.typ("mine")
	h.keys("esc :")
	h.typ("w")
	h.keys("enter")
	h.contains("Changed on disk")
	h.keys("r")
	if h.a.view != viewEditor || h.a.edit.ed.Dirty() {
		t.Fatal("reload should keep editing a clean buffer")
	}
	if !strings.Contains(string(h.a.edit.ed.Text()), "theirs") {
		t.Fatal("reload did not load the disk version")
	}
}

func TestExternalEditorReloads(t *testing.T) {
	h := newHarness(t, harnessOpt{}).open("Inbox.md")
	h.host.run = func(cmd *exec.Cmd) error {
		return os.WriteFile(filepath.Join(h.dir, "Inbox.md"), []byte("# Inbox\n\nedited outside\n"), 0o644)
	}
	h.keys("E")
	if len(h.host.ran) != 1 {
		t.Fatal("E ran no editor")
	}
	h.contains("edited outside")
	// editor = external makes i do the same.
	h2 := newHarness(t, harnessOpt{ext: true}).open("Inbox.md")
	h2.keys("i")
	if len(h2.host.ran) != 1 || h2.a.view != viewReader {
		t.Fatal("editor=external: i did not run $EDITOR")
	}
}

func TestResizeTinyAndBack(t *testing.T) {
	h := newHarness(t, harnessOpt{w: 80, h: 24}).open("Home.md")
	h.a.handle(term.ResizeEvent{Width: 30, Height: 8})
	h.vt.Resize(30, 8)
	h.frame()
	h.contains("window too small")
	h.keys("j k ctrl+p") // ignored, no crash
	if len(h.a.overlays) != 0 {
		t.Fatal("overlay opened in a too-small window")
	}
	h.a.handle(term.ResizeEvent{Width: 40, Height: 10})
	h.vt.Resize(40, 10)
	h.frame()
	h.lacks("window too small").contains("READ")
	h.a.handle(term.ResizeEvent{Width: 120, Height: 36})
	h.vt.Resize(120, 36)
	h.frame()
	h.contains("The front door of this vault")
}

func TestHelpIsGeneratedFromKeymap(t *testing.T) {
	h := newHarness(t, harnessOpt{w: 100, h: 40}).open("Home.md")
	h.keys("?")
	hp := h.a.overlays[0].(*help)
	descs := map[string]bool{}
	for _, l := range hp.lines {
		descs[l.desc] = true
	}
	for _, b := range keymap {
		if !descs[b.desc] {
			t.Fatalf("binding %v (%s) missing from help", b.keys, b.desc)
		}
	}
	h.contains("Keys", "cursor down")
	h.keys("G")
	h.contains("Editor")
	h.keys("esc")
	h.lacks("Keys")
}

func TestLeaderPaletteAfterPause(t *testing.T) {
	h := newHarness(t, harnessOpt{}).open("Home.md")
	h.fast("space")
	h.lacks("agenda")
	h.waitLeader()
	h.contains("Space", "find", "capture", "agenda", "theme")
	h.keys("f")
	h.contains("Find note")
	if _, ok := h.a.overlays[0].(*finder); !ok || len(h.a.overlays) != 1 {
		t.Fatalf("overlays after Space f: %#v", h.a.overlays)
	}
}

func TestLeaderImmediateDispatch(t *testing.T) {
	h := newHarness(t, harnessOpt{}).open("Home.md")
	h.fast("space t")
	if len(h.a.overlays) != 1 {
		t.Fatalf("overlays: %d", len(h.a.overlays))
	}
	if _, ok := h.a.overlays[0].(*agenda); !ok {
		t.Fatalf("Space t opened %T", h.a.overlays[0])
	}
	h.contains("Agenda", "Overdue")
}

func TestVaultSearchOpensAtLine(t *testing.T) {
	h := newHarness(t, harnessOpt{}).open("Home.md")
	h.keys("space /")
	h.contains("Search the vault")
	h.typ("sextant mirror")
	h.contains("matches")
	o := h.a.overlays[0].(*vaultSearch)
	if len(o.results) == 0 {
		t.Fatal("no results")
	}
	want := o.results[0]
	h.keys("enter")
	if h.a.doc.path != want.Path {
		t.Fatalf("opened %q, want %q", h.a.doc.path, want.Path)
	}
	if src := h.a.doc.page.SourceLine(h.a.doc.cur); src+1 != want.Line && want.Line > 0 {
		t.Fatalf("cursor on source line %d, want %d", src+1, want.Line)
	}
}

func TestVaultSearchDebounceAndCancel(t *testing.T) {
	h := newHarness(t, harnessOpt{}).open("Home.md")
	h.keys("space s")
	h.fast("k e s")
	o := h.a.overlays[0].(*vaultSearch)
	if o.at.IsZero() {
		t.Fatal("no debounced search pending")
	}
	h.keys("esc")
	if len(h.a.overlays) != 0 || h.a.jobs != 0 {
		t.Fatal("search not closed cleanly")
	}
}

func TestInNoteSearch(t *testing.T) {
	h := newHarness(t, harnessOpt{}).open("projects/Lantern migration.md")
	h.keys("/")
	h.typ("kestrel")
	h.keys("enter")
	d := h.a.doc
	if len(d.matches) < 2 {
		t.Fatalf("%d matches", len(d.matches))
	}
	first := d.cur
	h.keys("n")
	if d.cur == first && d.matches[d.mi].line == first {
		// Two matches on one line are fine; the index must still move.
		if d.mi == 0 {
			t.Fatal("n did not advance")
		}
	}
	h.keys("N")
	if !strings.Contains(h.statusLine(), "of") {
		t.Fatalf("status %q", h.statusLine())
	}
	// The match is drawn in the search style.
	m := d.matches[d.mi]
	st := h.vt.Style(m.x0, 1+m.line-d.top)
	if st.BG != h.a.th.SearchCurrent.BG {
		t.Fatalf("match style %+v", st)
	}
}

func TestCommandLineOpenAndComplete(t *testing.T) {
	h := newHarness(t, harnessOpt{}).open("Home.md")
	h.keys(":")
	h.typ("e Tidew")
	h.keys("tab")
	if got := h.a.prompt.in.text; got != "e projects/Tidewater" {
		t.Fatalf("completion = %q", got)
	}
	h.keys("enter")
	if h.a.doc.path != "projects/Tidewater.md" {
		t.Fatalf(":e opened %q", h.a.doc.path)
	}
	h.keys(":")
	h.typ("12")
	h.keys("enter")
	if h.a.doc.page.SourceLine(h.a.doc.cur) < 11 {
		t.Fatalf(":12 went to source line %d", h.a.doc.page.SourceLine(h.a.doc.cur))
	}
	h.keys(":")
	h.typ("nosuch")
	h.keys("enter")
	if !strings.Contains(h.statusLine(), "unknown command") {
		t.Fatalf("status %q", h.statusLine())
	}
}

func TestThemeCyclePersists(t *testing.T) {
	h := newHarness(t, harnessOpt{}).open("Home.md")
	h.keys("space T")
	if h.a.base.Name != theme.Next(theme.IronGall.Name) {
		t.Fatalf("theme %q", h.a.base.Name)
	}
	b, err := os.ReadFile(filepath.Join(h.state, "config"))
	if err != nil || !strings.Contains(string(b), "theme = "+h.a.base.Name) {
		t.Fatalf("config: %q %v", b, err)
	}
	// The page ground follows the theme.
	if got := h.vt.Style(0, 5).BG; got != h.a.th.Ground {
		t.Fatalf("ground %v, want %v", got, h.a.th.Ground)
	}
}

func TestTreeAndContextPanels(t *testing.T) {
	h := newHarness(t, harnessOpt{w: 130, h: 36}).open("projects/Lantern migration.md")
	// ≥ 124 columns: the context panel is open by default.
	h.contains("Outline", "Backlinks")
	h.keys("space e")
	h.contains("projects/", "Tidewater")
	if !h.a.tree.focus {
		t.Fatal("tree not focused")
	}
	// Move to Tidewater (the next row) and open it.
	h.keys("j enter")
	if h.a.doc.path != "projects/Tidewater.md" {
		t.Fatalf("tree opened %q", h.a.doc.path)
	}
	h.keys("space b")
	if h.a.ctx.focus != ctxBacklinks {
		t.Fatal("backlinks not focused")
	}
	h.keys("enter")
	if h.a.doc.path == "projects/Tidewater.md" {
		t.Fatal("Enter on a backlink did not open it")
	}
	// Outline focus jumps to a heading.
	h.a.openNote("projects/Lantern migration.md", 0)
	h.frame()
	h.keys("space o j enter")
	if !strings.Contains(h.cursorText(), "Why now") {
		t.Fatalf("outline jump landed on %q", h.cursorText())
	}
}

func TestNarrowWindowPanelsOverlay(t *testing.T) {
	h := newHarness(t, harnessOpt{w: 70, h: 24}).open("Home.md")
	full := h.a.pageWidth()
	h.keys("space e")
	if h.a.pageWidth() != full {
		t.Fatal("the tree squeezed the page in a narrow window")
	}
	l := h.a.layout()
	if !l.treeOver {
		t.Fatal("tree should overlay at 70 columns")
	}
}

func TestNarrowStatusBar(t *testing.T) {
	h := newHarness(t, harnessOpt{w: 50, h: 14}).open("projects/Lantern migration.md")
	st := h.statusLine()
	if strings.Contains(st, "words") || strings.Contains(st, "projects") {
		t.Fatalf("narrow status keeps more than name and pill: %q", st)
	}
	if !strings.Contains(st, "Lantern migration") || !strings.Contains(st, "READ") {
		t.Fatalf("narrow status %q", st)
	}
}

func TestHomeScreen(t *testing.T) {
	h := newHarness(t, harnessOpt{w: 100, h: 34})
	h.start(cli.ModeDefault, "", 0)
	h.contains("✦", "The vault is open.", "notes", "Recent", "find a note")
	h.keys("enter")
	if h.a.view != viewReader || h.a.doc == nil {
		t.Fatal("Enter on the home screen opened nothing")
	}
}

func TestStdinIsReadOnly(t *testing.T) {
	h := newHarness(t, harnessOpt{})
	h.a.start(cli.TUIRequest{Mode: cli.ModeStdin, Source: []byte("# Piped\n\n- [ ] a task\n\nSee [[Home]].\n"), Name: "-"})
	h.frame()
	h.contains("Piped", "a task")
	h.gotoText("a task")
	h.keys("x")
	if !strings.Contains(h.statusLine(), "read-only") {
		t.Fatalf("status %q", h.statusLine())
	}
	h.keys("i")
	if h.a.view != viewReader {
		t.Fatal("stdin page opened in the editor")
	}
	h.gotoText("See")
	h.keys("enter")
	if h.a.doc.path != "Home.md" {
		t.Fatalf("link from stdin opened %q", h.a.doc.path)
	}
	h.keys("backspace")
	if !h.a.doc.stdin {
		t.Fatal("back did not return to the stdin page")
	}
}

func TestPickReturnsPathOrCancels(t *testing.T) {
	h := newHarness(t, harnessOpt{})
	h.a.start(cli.TUIRequest{Mode: cli.ModePick, Query: "backpress"})
	h.frame()
	h.contains("Backpressure")
	h.keys("enter")
	if !h.a.quit || h.a.result.Path != "notes/Backpressure.md" || h.a.err != nil {
		t.Fatalf("pick: quit=%v path=%q err=%v", h.a.quit, h.a.result.Path, h.a.err)
	}
	h2 := newHarness(t, harnessOpt{})
	h2.a.start(cli.TUIRequest{Mode: cli.ModePick})
	h2.frame()
	h2.keys("esc")
	if !h2.a.quit || !errors.Is(h2.a.err, cli.ErrCancelled) {
		t.Fatalf("esc: quit=%v err=%v", h2.a.quit, h2.a.err)
	}
}

func TestNewNotePrompt(t *testing.T) {
	h := newHarness(t, harnessOpt{}).open("Home.md")
	h.keys("space n")
	h.typ("ideas/Garden plan")
	h.contains("creates ideas/Garden plan.md")
	h.keys("enter")
	if h.a.view != viewEditor || h.a.edit.doc.path != "ideas/Garden plan.md" {
		t.Fatal("new note did not open in the editor")
	}
	// A new note opens ready to write: Insert mode, one blank line below
	// the title.
	if m := h.a.edit.ed.Mode(); m != editor.ModeInsert {
		t.Fatalf("new note opened in %v, want INSERT", m)
	}
	h.typ("first line")
	h.keys("esc esc")
	if got := h.read("ideas/Garden plan.md"); got != "# Garden plan\n\nfirst line\n" {
		t.Fatalf("new note:\n%q", got)
	}
	// Leaving a fresh note without typing rewrites nothing.
	h.keys("space n")
	h.typ("Empty one")
	h.keys("enter")
	before := h.read("Empty one.md")
	h.keys("esc esc")
	if h.a.view == viewEditor {
		t.Fatal("Esc Esc did not leave the untouched new note")
	}
	if got := h.read("Empty one.md"); got != before || got != "# Empty one\n\n" {
		t.Fatalf("untouched new note was rewritten: %q", got)
	}
}

func TestAgendaToggleAndJump(t *testing.T) {
	h := newHarness(t, harnessOpt{w: 100, h: 34}).open("Home.md")
	h.keys("space t")
	o := h.a.overlays[0].(*agenda)
	ref := *o.current()
	h.keys("x")
	if !strings.Contains(h.read(ref.Path), "[x] "+ref.Task.Raw[strings.Index(ref.Task.Raw, "]")+2:]) {
		// Raw holds the whole line; check the state char directly instead.
		lines := strings.Split(h.read(ref.Path), "\n")
		if !strings.Contains(lines[ref.Task.Line-1], "[x]") {
			t.Fatalf("task not toggled: %q", lines[ref.Task.Line-1])
		}
	}
	h.contains("☑") // stays visible, struck through
	h.keys("enter")
	if h.a.doc.path != ref.Path {
		t.Fatalf("Enter opened %q, want %q", h.a.doc.path, ref.Path)
	}
}

// At 60 columns the agenda drops its note column so task text stays
// readable; the selected task's note is named in the header.
func TestAgendaNarrow(t *testing.T) {
	dir := copyVault(t)
	os.WriteFile(filepath.Join(dir, "Narrow.md"), []byte("# Narrow\n\n- [ ] Call **the** vendor about the [[Inbox|renewal]] terms 📅 2026-10-04\n"), 0o644)
	h := newHarness(t, harnessOpt{w: 60, h: 24, dir: dir}).open("Home.md")
	h.keys("space t")
	h.contains("Call the vendor about the renewal terms")
	h.lacks("**the**", "[[Inbox")
	o := h.a.overlays[0].(*agenda)
	for o.current() == nil || o.current().Path != "Narrow.md" {
		before := o.list.sel
		h.keys("down")
		if o.list.sel == before {
			t.Fatal("task not in the agenda")
		}
	}
	h.contains("Narrow") // its note, in the header
	// Wide: the note column is back.
	h2 := newHarness(t, harnessOpt{w: 100, h: 30, dir: dir}).open("Home.md")
	h2.keys("space t")
	h2.contains("x toggles")
}

func TestTagsBrowser(t *testing.T) {
	h := newHarness(t, harnessOpt{}).open("Home.md")
	h.keys("space #")
	h.contains("Tags", "#")
	h.typ("runbook")
	h.keys("enter")
	h.contains("#runbook", "Lantern cutover")
	h.keys("enter")
	if h.a.doc.path != "runbooks/Lantern cutover.md" {
		t.Fatalf("opened %q", h.a.doc.path)
	}
}

func TestCodeBlockScrollsHorizontally(t *testing.T) {
	dir := t.TempDir()
	long := strings.Repeat("abcdefghij", 20)
	os.WriteFile(filepath.Join(dir, "Code.md"), []byte("# Code\n\n```sh\necho "+long+"\n```\n"), 0o644)
	h := newHarness(t, harnessOpt{dir: dir, w: 60, h: 20}).open("Code.md")
	h.gotoText("echo")
	h.keys("l l")
	if h.a.doc.codeOff != 8 {
		t.Fatalf("code offset %d", h.a.doc.codeOff)
	}
	h.lacks("echo abc")
	h.keys("h h")
	h.contains("echo abc")
}

func TestEmptyVaultAndOddFiles(t *testing.T) {
	dir := t.TempDir()
	h := newHarness(t, harnessOpt{dir: dir})
	h.start(cli.ModeDefault, "", 0)
	h.contains("The vault is open.", "0 notes")
	h.keys("ctrl+p")
	h.contains("no notes")
	h.keys("esc space t")
	h.contains("no open tasks")
	h.keys("esc space #")
	h.contains("no tags")
	h.keys("esc space /")
	h.typ("anything")
	h.contains("nothing matches")
	h.keys("esc")

	// Binary content with a .md name, an unreadable file and a big note.
	os.WriteFile(filepath.Join(dir, "blob.md"), []byte{0x7f, 'E', 'L', 'F', 0, 0, 1, 2, 3}, 0o644)
	big := strings.Repeat("A paragraph of prose that goes on for a while, with *emphasis* and [[links]].\n\n", 1300)
	os.WriteFile(filepath.Join(dir, "big.md"), []byte("# Big\n\n"+big), 0o644)
	os.WriteFile(filepath.Join(dir, "secret.md"), []byte("# s\n"), 0o000)
	h.a.v.Rescan(context.Background())
	h.a.openNote("blob.md", 0)
	h.frame()
	h.contains("Not a text file")
	h.keys("i")
	if h.a.view == viewEditor {
		t.Fatal("binary file opened in the editor")
	}
	t0 := time.Now()
	h.a.openNote("big.md", 0)
	h.frame()
	h.keys("G g g ctrl+d ctrl+d")
	if el := time.Since(t0); el > ciSlack(2*time.Second) {
		t.Fatalf("100 KB note took %v", el)
	}
	if os.Getuid() != 0 {
		if h.a.openNote("secret.md", 0) {
			t.Fatal("opened an unreadable file")
		}
		h.frame()
		if !strings.Contains(h.statusLine(), "cannot open") {
			t.Fatalf("status %q", h.statusLine())
		}
	}
}

func TestMouseWheelAndClick(t *testing.T) {
	h := newHarness(t, harnessOpt{w: 100, h: 20}).open("projects/Lantern migration.md")
	h.a.handle(term.MouseEvent{Button: term.MouseWheelDown, X: 50, Y: 5})
	h.frame()
	if h.a.doc.top != 3 {
		t.Fatalf("wheel scrolled to %d", h.a.doc.top)
	}
	// Click on a link follows it.
	d := h.a.doc
	h.a.handle(term.MouseEvent{Button: term.MouseWheelUp, X: 50, Y: 5})
	h.frame()
	for i, l := range d.page.Links {
		if l.Status == render.LinkResolved && strings.HasSuffix(l.Path, ".md") && len(l.Regions) > 0 {
			r := l.Regions[0]
			if r.Line-d.top < 0 || r.Line-d.top >= h.a.viewRows() {
				continue
			}
			h.a.handle(term.MouseEvent{Button: term.MouseLeft, Action: term.MousePress, X: r.X0, Y: 1 + r.Line - d.top})
			h.frame()
			if h.a.doc.path != l.Path {
				t.Fatalf("click on link %d opened %q, want %q", i, h.a.doc.path, l.Path)
			}
			return
		}
	}
	t.Skip("no clickable link in view")
}

func TestSuspendKey(t *testing.T) {
	h := newHarness(t, harnessOpt{}).open("Home.md")
	h.keys("ctrl+z")
	if h.host.suspended != 1 {
		t.Fatal("Ctrl-Z did not suspend")
	}
}

func TestASCIIAndSixteenColours(t *testing.T) {
	h := newHarness(t, harnessOpt{glyphs: theme.ASCIIGlyphs, profile: term.Profile16}).open("Home.md")
	s := h.screen()
	for _, r := range s {
		if r > 0x7e && r != '—' && r != '…' { // note text may carry its own punctuation
			if strings.ContainsRune("✦▎▾▸•◦▪☐☑─│", r) {
				t.Fatalf("Unicode glyph %q in ASCII mode:\n%s", r, s)
			}
		}
	}
	h.contains("* Home")
	h.keys("ctrl+p")
	h.contains("Find note", "+--")
}

// TestLoopQuitsWithoutLeaks drives the real event loop with a scripted event
// stream and checks it returns with no goroutine left behind.
func TestLoopQuitsWithoutLeaks(t *testing.T) {
	before := runtime.NumGoroutine()
	h := newHarness(t, harnessOpt{}).open("Home.md")
	events := make(chan term.Event, 64)
	for _, k := range []string{"j", "j", "space", "/"} {
		events <- keyEvent(k)
	}
	for _, r := range "lantern" {
		events <- term.KeyEvent{Key: term.KeyRune, Rune: r}
	}
	// A resize storm, then quit.
	for i := 0; i < 50; i++ {
		events <- term.ResizeEvent{Width: 80 + i%20, Height: 24 + i%5}
	}
	events <- keyEvent("esc")
	events <- keyEvent("q")
	done := make(chan struct{})
	flushes := 0
	go func() {
		h.a.loop(context.Background(), events, func() error { flushes++; return h.scr.Flush() })
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("loop did not quit")
	}
	if flushes > 10 {
		t.Fatalf("%d frames for one burst of events: not coalesced", flushes)
	}
	time.Sleep(50 * time.Millisecond)
	if after := runtime.NumGoroutine(); after > before+1 {
		t.Fatalf("goroutines: %d before, %d after", before, after)
	}
}

func TestRecentFileRemembersLine(t *testing.T) {
	h := newHarness(t, harnessOpt{}).open("projects/Lantern migration.md")
	h.keys("G")
	src := h.a.doc.page.SourceLine(h.a.doc.cur) + 1
	h.a.finish()
	b, err := os.ReadFile(filepath.Join(h.state, "recent"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "Lantern migration.md") || !strings.HasPrefix(string(b), itoa(src)+"\t") {
		t.Fatalf("recent file:\n%s", b)
	}
}

func TestPlainContextTableRow(t *testing.T) {
	got := plainContext("| Concepts | [[Backpressure]], [[Idempotency keys\\|keys]] | `a|b` |")
	want := "Concepts · Backpressure, keys · a|b"
	if got != want {
		t.Errorf("plainContext(table row) = %q, want %q", got, want)
	}
	if got := plainContext("- see [[Backpressure]] for **why**"); got != "see Backpressure for why" {
		t.Errorf("plainContext(list item) = %q", got)
	}
}

// Several links from one note are listed under its title once, and the
// panel counts notes, as the title block's "N backlinks" chip does.
func TestBacklinksGroupedByNote(t *testing.T) {
	h := newHarness(t, harnessOpt{w: 150, h: 60}).open("runbooks/Lantern cutover.md")
	screen := h.vt.String()
	n := h.a.v.BacklinkCount("runbooks/Lantern cutover.md")
	if !strings.Contains(screen, "Backlinks  "+itoa(n)) || !strings.Contains(screen, itoa(n)+" backlinks") {
		t.Fatalf("backlink counts disagree (want %d):\n%s", n, screen)
	}
	panel := 0
	for _, l := range h.vt.Lines() {
		if i := strings.Index(l, "│"); i >= 0 && strings.TrimSpace(l[strings.LastIndex(l, "│")+len("│"):]) == "On-call" {
			panel++
		}
	}
	if panel != 1 {
		t.Errorf("On-call listed %d times in the backlinks panel, want once:\n%s", panel, screen)
	}
}

// The reader cursor passes over the title hairline like a blank row.
func TestCursorSkipsHairline(t *testing.T) {
	h := newHarness(t, harnessOpt{w: 100, h: 30}).open("runbooks/Lantern cutover.md")
	h.keys("g g j j")
	l := h.a.doc.page.Lines[h.a.doc.cur]
	if txt := strings.TrimSpace(l.Text()); !strings.Contains(txt, "properties") {
		t.Fatalf("after gg j j the cursor is on %q, want the properties line", txt)
	}
}

// ciSlack widens timing budgets on shared CI runners (CI is set by GitHub
// Actions and most CI systems), whose noisy neighbours make wall-clock
// checks flaky; locally the strict budget applies.
func ciSlack(d time.Duration) time.Duration {
	if os.Getenv("CI") != "" {
		return 10 * d
	}
	return d
}

func TestReadingTime(t *testing.T) {
	for words, want := range map[int]string{
		0: "1 min", 1: "1 min", wordsPerMinute: "1 min", wordsPerMinute + 1: "2 min",
		59 * wordsPerMinute: "59 min", 60 * wordsPerMinute: "1 h", 61 * wordsPerMinute: "1 h 1 min",
		(54*60 + 49) * wordsPerMinute: "54 h 49 min", 1200 * 60 * wordsPerMinute: "1,200 h",
	} {
		if got := readingTime(words); got != want {
			t.Errorf("readingTime(%d) = %q, want %q", words, got, want)
		}
	}
}

// The reader's cursor lands only on code rows with code on them: the
// padding rows, the language label and blank code lines are skipped like
// hairlines. A block with no code stays reachable.
func TestCursorSkipsCodePadding(t *testing.T) {
	dir := copyVault(t)
	os.WriteFile(filepath.Join(dir, "Code.md"), []byte("# Code\n\nbefore\n\n```go\nx := 1\n\ny := 2\n```\n\nmiddle\n\n```\n```\n\nafter\n"), 0o644)
	h := newHarness(t, harnessOpt{dir: dir}).open("Code.md")
	h.gotoText("before")
	for _, want := range []string{"x := 1", "y := 2", "middle", "", "after"} {
		h.keys("j")
		got := strings.TrimSpace(h.cursorText())
		if want == "" {
			if l := h.a.doc.page.Lines[h.a.doc.cur]; l.Kind != render.KindCode {
				t.Fatalf("empty code block skipped: on %q", got)
			}
			continue
		}
		if !strings.Contains(got, want) {
			t.Fatalf("j landed on %q, want %q", got, want)
		}
	}
	for _, want := range []string{"", "middle", "y := 2", "x := 1", "before"} {
		h.keys("k")
		if want != "" && !strings.Contains(h.cursorText(), want) {
			t.Fatalf("k landed on %q, want %q", h.cursorText(), want)
		}
	}
}

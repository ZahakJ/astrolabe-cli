package vault

import (
	"testing"
)

func TestResolveLink(t *testing.T) {
	v := openScanned(t, map[string]string{
		"Home.md":               "# Home",
		"projects/Home.md":      "# Projects home",
		"projects/plan.md":      "---\naliases: [Roadmap]\n---\n# Plan\n## Next Steps!\ntext ^b1\n",
		"archive/2020/plan.md":  "# Old plan",
		"a/deep/Unique Name.md": "# U",
		"x/Same.md":             "1",
		"y/Same.md":             "2",
		"journal/today.md":      "x",
		"journal/sibling.md":    "x",
		"assets/pic.png":        "png",
		"ملاحظات/فكرة.md":       "# فكرة",
	})
	cases := []struct {
		from, target string
		kind         LinkKind
		heading      string
		block        string
		want         string
		line         int
		ok           bool
	}{
		{"journal/today.md", "Home", KindWikilink, "", "", "Home.md", 0, true},                   // root exact beats deeper
		{"journal/today.md", "projects/Home", KindWikilink, "", "", "projects/Home.md", 0, true}, // folder path
		{"journal/today.md", "plan", KindWikilink, "", "", "projects/plan.md", 0, true},          // shortest path
		{"journal/today.md", "2020/plan", KindWikilink, "", "", "archive/2020/plan.md", 0, true}, // suffix match
		{"journal/today.md", "unique name", KindWikilink, "", "", "a/deep/Unique Name.md", 0, true},
		{"journal/today.md", "Unique Name.md", KindWikilink, "", "", "a/deep/Unique Name.md", 0, true},
		{"journal/today.md", "Same", KindWikilink, "", "", "x/Same.md", 0, true}, // tie: lexicographic
		{"journal/today.md", "roadmap", KindWikilink, "", "", "projects/plan.md", 0, true},
		{"journal/today.md", "plan", KindWikilink, "next steps", "", "projects/plan.md", 5, true},
		{"journal/today.md", "plan", KindWikilink, "Next Steps!", "", "projects/plan.md", 5, true},
		{"journal/today.md", "plan", KindWikilink, "Plan#Next Steps", "", "projects/plan.md", 5, true},
		{"journal/today.md", "plan", KindWikilink, "", "b1", "projects/plan.md", 6, true},
		{"journal/today.md", "sibling", KindWikilink, "", "", "journal/sibling.md", 0, true},
		{"journal/today.md", "sibling.md", KindMarkdown, "", "", "journal/sibling.md", 0, true},
		{"journal/today.md", "../Home.md", KindMarkdown, "", "", "Home.md", 0, true},
		{"projects/plan.md", "Home.md", KindMarkdown, "", "", "projects/Home.md", 0, true}, // md: relative first
		{"projects/plan.md", "Home", KindWikilink, "", "", "Home.md", 0, true},             // wiki: root first
		{"journal/today.md", "pic.png", KindEmbed, "", "", "assets/pic.png", 0, true},
		{"journal/today.md", "pic", KindEmbed, "", "", "", 0, false}, // attachments need the extension
		{"journal/today.md", "فكرة", KindWikilink, "", "", "ملاحظات/فكرة.md", 0, true},
		{"journal/today.md", "Missing", KindWikilink, "", "", "", 0, false},
		{"projects/plan.md", "", KindWikilink, "Plan", "", "projects/plan.md", 4, true}, // self
	}
	for _, c := range cases {
		l := Link{Kind: c.kind, Target: c.target, Heading: c.heading, Block: c.block, Wiki: c.kind != KindMarkdown}
		got, ok := v.ResolveLink(c.from, l)
		if ok != c.ok || got.Path != c.want || got.Line != c.line {
			t.Errorf("%s → %q#%s^%s: got %+v %v, want %q line %d %v", c.from, c.target, c.heading, c.block, got, ok, c.want, c.line, c.ok)
		}
	}
	// Missing anchor still resolves the note.
	got, ok := v.ResolveLink("Home.md", Link{Kind: KindWikilink, Wiki: true, Target: "plan", Heading: "Nope"})
	if !ok || !got.AnchorMissing || got.Path != "projects/plan.md" {
		t.Errorf("missing anchor: %+v %v", got, ok)
	}
	if _, ok := v.ResolveLink("Home.md", Link{Kind: KindURL, Target: "https://x"}); ok {
		t.Error("URL resolved")
	}
	if rel, ok := v.ResolveName("[[Roadmap]]"); !ok || rel != "projects/plan.md" {
		t.Errorf("ResolveName alias: %q %v", rel, ok)
	}
	if _, ok := v.ResolveName("pic.png"); ok {
		t.Error("ResolveName returned an attachment")
	}
}

func TestBacklinksAndUnresolved(t *testing.T) {
	v := openScanned(t, map[string]string{
		"target.md": "# Target\n## Part\nSee [[#Part]] here.\n",
		"a.md":      "Intro line\n- links to [[Target]] in a list\n",
		"b.md":      "First [[target#Part|the part]].\nThen [md](target.md) and [[Ghost]] and ![[ghost.png]].\n",
		"c.md":      "[[Ghost]] again and https://example.com\n",
	})
	bl := v.Backlinks("target.md")
	if len(bl) != 3 {
		t.Fatalf("backlinks %+v", bl)
	}
	if bl[0].From != "a.md" || bl[0].Link.Line != 2 || bl[0].Link.Context != "- links to [[Target]] in a list" {
		t.Errorf("bl0 %+v", bl[0])
	}
	if bl[1].From != "b.md" || bl[1].Link.Label != "the part" || bl[2].Link.Kind != KindMarkdown {
		t.Errorf("bl1/2 %+v %+v", bl[1], bl[2])
	}
	if v.BacklinkCount("target.md") != 2 {
		t.Errorf("count %d", v.BacklinkCount("target.md"))
	}
	un := v.Unresolved()
	if len(un) != 3 || un[0].From != "b.md" || un[0].Link.Target != "Ghost" || un[2].From != "c.md" {
		t.Errorf("unresolved %+v", un)
	}
	ut := v.UnresolvedTargets()
	if len(ut) != 2 || ut[0] != (TargetCount{"Ghost", 2}) || ut[1] != (TargetCount{"ghost.png", 1}) {
		t.Errorf("targets %+v", ut)
	}
	// Creating the missing note resolves the links (cache invalidated).
	writeTree(t, v.Root(), map[string]string{"Ghost.md": "# Ghost"})
	v.Refresh("Ghost.md")
	if len(v.Backlinks("Ghost.md")) != 2 || len(v.Unresolved()) != 1 {
		t.Errorf("after create: %d backlinks, %d unresolved", len(v.Backlinks("Ghost.md")), len(v.Unresolved()))
	}
}

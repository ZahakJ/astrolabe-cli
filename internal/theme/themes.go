package theme

// The four built-in themes. Every theme is authored in 24-bit colour; the
// 256-colour profile derives from these by nearest colour, the 16-colour and
// monochrome profiles use Basic16 and Mono.
//
// Contrast floors (DESIGN.md §7, enforced by tests): Text ≥ 7:1 on Ground,
// Muted ≥ 4.5:1, Faint ≥ 3:1.

// IronGall is the default and the brand: a warm near-black room lit by gold
// leaf, ink the colour of old ivory — iron-gall ink read by lamplight.
var IronGall = register(Theme{
	Name:       "iron-gall",
	Dark:       true,
	Ground:     Hex("#16130e"),
	Raised:     Hex("#1f1b14"),
	Hover:      Hex("#2b2516"),
	Text:       Hex("#eae2d0"),
	Muted:      Hex("#a39a85"),
	Faint:      Hex("#746b59"),
	Heading:    Hex("#ead7a4"),
	Accent:     Hex("#c9a227"),
	AccentSoft: Hex("#3a3018"),
	Border:     Hex("#3d3427"),
	Danger:     Hex("#d06a52"),
	Ok:         Hex("#97ad72"),
	Link:       Hex("#d1ad3e"),
	Math:       Hex("#b9b08f"),

	CodeComment: Hex("#857b67"),
	CodeString:  Hex("#a8b573"),
	CodeNumber:  Hex("#d6955f"),
	CodeKeyword: Hex("#c99a8a"),

	Callout: Callouts{
		Note:     Hex("#8fa8c2"),
		Abstract: Hex("#86b3ab"),
		Info:     Hex("#8fa8c2"),
		Todo:     Hex("#8fa8c2"),
		Tip:      Hex("#7fb39b"),
		Success:  Hex("#97ad72"),
		Question: Hex("#d4ab5a"),
		Warning:  Hex("#da9a4a"),
		Failure:  Hex("#d06a52"),
		Danger:   Hex("#d06a52"),
		Bug:      Hex("#c87070"),
		Example:  Hex("#a993c4"),
		Quote:    Hex("#a39a85"),
	},

	Selection:     Style{FG: Hex("#f4ecdb"), BG: Hex("#4a3d1c")},
	CursorLine:    Style{BG: Hex("#1d1912")},
	Highlight:     Style{FG: Hex("#f4ecdb"), BG: Hex("#3a3018")},
	LinkFocus:     Style{FG: Hex("#16130e"), BG: Hex("#c9a227")},
	Search:        Style{FG: Hex("#f4ecdb"), BG: Hex("#5a4816")},
	SearchCurrent: Style{FG: Hex("#16130e"), BG: Hex("#e0b93a"), Attrs: Bold},
	StatusBar:     Style{FG: Hex("#a39a85"), BG: Hex("#1f1b14")},
	PillRead:      Style{FG: Hex("#16130e"), BG: Hex("#c9a227"), Attrs: Bold},
	PillNormal:    Style{FG: Hex("#16130e"), BG: Hex("#97ad72"), Attrs: Bold},
	PillInsert:    Style{FG: Hex("#16130e"), BG: Hex("#d6955f"), Attrs: Bold},
	PillVisual:    Style{FG: Hex("#16130e"), BG: Hex("#a993c4"), Attrs: Bold},
})

// Parchment is the light room: laid paper, sepia ink, an aged-gold accent.
var Parchment = register(Theme{
	Name:       "parchment",
	Dark:       false,
	Ground:     Hex("#f2ebda"),
	Raised:     Hex("#e8dfc9"),
	Hover:      Hex("#e6d7ad"),
	Text:       Hex("#33291a"),
	Muted:      Hex("#66583f"),
	Faint:      Hex("#8a7c63"),
	Heading:    Hex("#4b3410"),
	Accent:     Hex("#7a5f14"),
	AccentSoft: Hex("#ead796"),
	Border:     Hex("#cfc2a3"),
	Danger:     Hex("#a3361f"),
	Ok:         Hex("#4d6a28"),
	Link:       Hex("#7a5f14"),
	Math:       Hex("#5d5a3a"),

	CodeComment: Hex("#857760"),
	CodeString:  Hex("#566a1c"),
	CodeNumber:  Hex("#98501a"),
	CodeKeyword: Hex("#7d3a5e"),

	Callout: Callouts{
		Note:     Hex("#3f6185"),
		Abstract: Hex("#2f6f68"),
		Info:     Hex("#3f6185"),
		Todo:     Hex("#3f6185"),
		Tip:      Hex("#2f6f55"),
		Success:  Hex("#4d6a28"),
		Question: Hex("#8a6410"),
		Warning:  Hex("#9a5a12"),
		Failure:  Hex("#a3361f"),
		Danger:   Hex("#a3361f"),
		Bug:      Hex("#9a3a3a"),
		Example:  Hex("#634a8a"),
		Quote:    Hex("#66583f"),
	},

	Selection:     Style{FG: Hex("#2a2114"), BG: Hex("#dcc98e")},
	CursorLine:    Style{BG: Hex("#ebe3cf")},
	Highlight:     Style{FG: Hex("#2a2114"), BG: Hex("#ead796")},
	LinkFocus:     Style{FG: Hex("#f2ebda"), BG: Hex("#7a5f14")},
	Search:        Style{FG: Hex("#2a2114"), BG: Hex("#e3c766")},
	SearchCurrent: Style{FG: Hex("#f2ebda"), BG: Hex("#8a6410"), Attrs: Bold},
	StatusBar:     Style{FG: Hex("#66583f"), BG: Hex("#e8dfc9")},
	PillRead:      Style{FG: Hex("#f2ebda"), BG: Hex("#7a5f14"), Attrs: Bold},
	PillNormal:    Style{FG: Hex("#f2ebda"), BG: Hex("#4d6a28"), Attrs: Bold},
	PillInsert:    Style{FG: Hex("#f2ebda"), BG: Hex("#98501a"), Attrs: Bold},
	PillVisual:    Style{FG: Hex("#f2ebda"), BG: Hex("#634a8a"), Attrs: Bold},
})

// Graphite is a neutral, cool dark room that keeps the gold accent.
var Graphite = register(Theme{
	Name:       "graphite",
	Dark:       true,
	Ground:     Hex("#0d1117"),
	Raised:     Hex("#161b22"),
	Hover:      Hex("#252417"),
	Text:       Hex("#e2e6ea"),
	Muted:      Hex("#9aa3ad"),
	Faint:      Hex("#6b7480"),
	Heading:    Hex("#ebdcb0"),
	Accent:     Hex("#c9a227"),
	AccentSoft: Hex("#2e2a17"),
	Border:     Hex("#30363d"),
	Danger:     Hex("#e0705c"),
	Ok:         Hex("#8fb573"),
	Link:       Hex("#d1ad3e"),
	Math:       Hex("#a9b4bf"),

	CodeComment: Hex("#7b8490"),
	CodeString:  Hex("#9fc08a"),
	CodeNumber:  Hex("#d9a066"),
	CodeKeyword: Hex("#c3a0d8"),

	Callout: Callouts{
		Note:     Hex("#79a6d9"),
		Abstract: Hex("#6fbab1"),
		Info:     Hex("#79a6d9"),
		Todo:     Hex("#79a6d9"),
		Tip:      Hex("#6fbf9c"),
		Success:  Hex("#8fb573"),
		Question: Hex("#d9b25e"),
		Warning:  Hex("#e09a4f"),
		Failure:  Hex("#e0705c"),
		Danger:   Hex("#e0705c"),
		Bug:      Hex("#d77a85"),
		Example:  Hex("#b19ad9"),
		Quote:    Hex("#9aa3ad"),
	},

	Selection:     Style{FG: Hex("#f0f3f6"), BG: Hex("#3b3520")},
	CursorLine:    Style{BG: Hex("#141a21")},
	Highlight:     Style{FG: Hex("#f0f3f6"), BG: Hex("#3b3520")},
	LinkFocus:     Style{FG: Hex("#0d1117"), BG: Hex("#c9a227")},
	Search:        Style{FG: Hex("#f0f3f6"), BG: Hex("#5a4a18")},
	SearchCurrent: Style{FG: Hex("#0d1117"), BG: Hex("#e0b93a"), Attrs: Bold},
	StatusBar:     Style{FG: Hex("#9aa3ad"), BG: Hex("#161b22")},
	PillRead:      Style{FG: Hex("#0d1117"), BG: Hex("#c9a227"), Attrs: Bold},
	PillNormal:    Style{FG: Hex("#0d1117"), BG: Hex("#8fb573"), Attrs: Bold},
	PillInsert:    Style{FG: Hex("#0d1117"), BG: Hex("#d9a066"), Attrs: Bold},
	PillVisual:    Style{FG: Hex("#0d1117"), BG: Hex("#b19ad9"), Attrs: Bold},
})

// Mocha matches the Catppuccin Mocha palette so folio sits inside a
// Catppuccin Neovim/tmux setup; mauve takes the accent's place.
var Mocha = register(Theme{
	Name:       "mocha",
	Dark:       true,
	Ground:     Hex("#1e1e2e"), // base
	Raised:     Hex("#181825"), // mantle
	Hover:      Hex("#313244"), // surface0
	Text:       Hex("#cdd6f4"), // text
	Muted:      Hex("#a6adc8"), // subtext0
	Faint:      Hex("#7f849c"), // overlay1
	Heading:    Hex("#b4befe"), // lavender
	Accent:     Hex("#cba6f7"), // mauve
	AccentSoft: Hex("#3b3452"),
	Border:     Hex("#45475a"), // surface1
	Danger:     Hex("#f38ba8"), // red
	Ok:         Hex("#a6e3a1"), // green
	Link:       Hex("#cba6f7"),
	Math:       Hex("#94e2d5"), // teal

	CodeComment: Hex("#9399b2"), // overlay2
	CodeString:  Hex("#a6e3a1"),
	CodeNumber:  Hex("#fab387"), // peach
	CodeKeyword: Hex("#cba6f7"),

	Callout: Callouts{
		Note:     Hex("#89b4fa"), // blue
		Abstract: Hex("#89dceb"), // sky
		Info:     Hex("#89b4fa"),
		Todo:     Hex("#89b4fa"),
		Tip:      Hex("#94e2d5"),
		Success:  Hex("#a6e3a1"),
		Question: Hex("#f9e2af"), // yellow
		Warning:  Hex("#fab387"),
		Failure:  Hex("#f38ba8"),
		Danger:   Hex("#f38ba8"),
		Bug:      Hex("#eba0ac"), // maroon
		Example:  Hex("#b4befe"),
		Quote:    Hex("#a6adc8"),
	},

	Selection:     Style{FG: Hex("#cdd6f4"), BG: Hex("#45475a")},
	CursorLine:    Style{BG: Hex("#252536")},
	Highlight:     Style{FG: Hex("#cdd6f4"), BG: Hex("#3b3452")},
	LinkFocus:     Style{FG: Hex("#1e1e2e"), BG: Hex("#cba6f7")},
	Search:        Style{FG: Hex("#cdd6f4"), BG: Hex("#585b70")},
	SearchCurrent: Style{FG: Hex("#1e1e2e"), BG: Hex("#f9e2af"), Attrs: Bold},
	StatusBar:     Style{FG: Hex("#a6adc8"), BG: Hex("#181825")},
	PillRead:      Style{FG: Hex("#1e1e2e"), BG: Hex("#cba6f7"), Attrs: Bold},
	PillNormal:    Style{FG: Hex("#1e1e2e"), BG: Hex("#a6e3a1"), Attrs: Bold},
	PillInsert:    Style{FG: Hex("#1e1e2e"), BG: Hex("#fab387"), Attrs: Bold},
	PillVisual:    Style{FG: Hex("#1e1e2e"), BG: Hex("#b4befe"), Attrs: Bold},
})

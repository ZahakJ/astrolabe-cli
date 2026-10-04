package tui

// The Astrolabe mark for the home screen: the brand logo (suspension ring,
// limb, two plate circles, the alidade with its pointers across the centre
// pin) rasterised into braille cells. Each art row has a class row: 'r' for
// the rings (accent), 'c' for the alidade (link ink), '.' for blank.
var logoArt = struct {
	rows, classes []string
}{
	rows: []string{
		"        ⢰⠋⠙⡆        ",
		"     ⣀⣤⠴⠶⠷⠾⠶⠦⣤⣀     ",
		"  ⢀⣴⠟⠉  ⣀⣀⣀⣀  ⠉⠻⣦⡀  ",
		" ⣠⠟⠁ ⡠⠚⠉    ⠉⠓⢄⣠⠜⠻⣄ ",
		"⢰⡏ ⢀⠞  ⡠⠖⠚⠓⠲⢄⡴⠊⠳⡀ ⢹⡆",
		"⣾  ⡼  ⡞  ⢀⣠⠴⠋⢳  ⢧  ⣷",
		"⣿  ⣇  ⣇ ⣠⠞⠃  ⣸  ⣸  ⣿",
		"⢸⡆ ⠸⡄⢀⡼⠯⣀⡀⢀⣀⠴⠃ ⢠⠇ ⢰⡇",
		" ⢻⣄⡠⠞⢯⣀  ⠉⠉  ⣀⡴⠃ ⣠⡟ ",
		"  ⠙⢧⣄ ⠈⠙⠒⠒⠒⠒⠋⠁ ⣠⡼⠋  ",
		"    ⠉⠻⠶⣤⣤⣀⣀⣤⣤⠶⠟⠉    ",
	},
	classes: []string{
		"........rrrr........",
		".....rrrrrrrrrr.....",
		"..rrrr..rrrr..rrrr..",
		".rrr.rrr....rrrcccr.",
		"rr.rr..rrrrrccccr.rr",
		"r..r..r..ccccr..r..r",
		"r..r..r.ccc..r..r..r",
		"rr.rrcccrrrrrr.rr.rr",
		".rccccr..rr..rrr.rr.",
		"..ccr.rrrrrrrr.rrr..",
		"....rrrrrrrrrrrr....",
	},
}

// logoASCII is the mark for --ascii and non-UTF-8 terminals.
var logoASCII = struct {
	rows, classes []string
}{
	rows: []string{
		"    o    ",
		" .-----. ",
		"/ .---./\\",
		"| |(*)/ |",
		"\\ '/--' /",
		" '-----' ",
	},
	classes: []string{
		"    r    ",
		" rrrrrrr ",
		"r rrrrrcr",
		"r rcccc r",
		"r rcrrr r",
		" rrrrrrr ",
	},
}

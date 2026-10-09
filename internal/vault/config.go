package vault

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// ConfigKeys are the keys of DESIGN.md §9.
var ConfigKeys = []string{
	"dir", "theme", "measure", "ground", "bidi", "mouse",
	"daily_dir", "daily_format", "editor", "ascii",
}

// ConfigDir returns $XDG_CONFIG_HOME/astrolabe-cli, else
// ~/.config/astrolabe-cli. Back-compat: when that directory is absent and
// the one of the tool's former name (folio) exists, that one is used as it
// is; nothing is moved or copied.
func ConfigDir(getenv func(string) string) string {
	base := filepath.Join(getenv("HOME"), ".config")
	if d := getenv("XDG_CONFIG_HOME"); d != "" && filepath.IsAbs(d) {
		base = d
	}
	return legacyDir(base, "astrolabe-cli", "folio")
}

// legacyDir returns base/name, or base/old when only that one exists.
func legacyDir(base, name, old string) string {
	dir := filepath.Join(base, name)
	if _, err := os.Stat(dir); err != nil {
		if fi, err := os.Stat(filepath.Join(base, old)); err == nil && fi.IsDir() {
			return filepath.Join(base, old)
		}
	}
	return dir
}

// ConfigFile returns the path of the optional config file.
func ConfigFile(getenv func(string) string) string {
	return filepath.Join(ConfigDir(getenv), "config")
}

// Config holds the optional configuration: "key = value" lines from the
// config file, with environment overrides ASTROLABE_<KEY> (key upper-cased).
type Config struct {
	// File is the path the config was read from.
	File   string
	values map[string]string
	getenv func(string) string
	// Unknown lists keys in the file that astrolabe does not know, for the
	// single warning in `astrolabe doctor`.
	Unknown []string
	// Bad lists malformed lines as "line N: text".
	Bad []string
}

// LoadConfig reads the config file; a missing file is not an error and
// yields an empty Config. getenv supplies the ASTROLABE_<KEY> overrides (nil
// means no environment).
func LoadConfig(file string, getenv func(string) string) (*Config, error) {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	c := &Config{File: file, values: map[string]string{}, getenv: getenv}
	data, err := os.ReadFile(file)
	if errors.Is(err, fs.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	known := map[string]bool{}
	for _, k := range ConfigKeys {
		known[k] = true
	}
	for i, line := range strings.Split(string(data), "\n") {
		key, val, ok, blank := parseConfigLine(line)
		if blank {
			continue
		}
		if !ok {
			c.Bad = append(c.Bad, fmt.Sprintf("line %d: %s", i+1, strings.TrimSpace(line)))
			continue
		}
		if !known[key] {
			c.Unknown = append(c.Unknown, key)
		}
		c.values[key] = val
	}
	sort.Strings(c.Unknown)
	return c, nil
}

// parseConfigLine parses "key = value" ('#' starts a comment line; a value
// may be quoted). blank is true for empty and comment lines.
func parseConfigLine(line string) (key, val string, ok, blank bool) {
	t := strings.TrimSpace(strings.TrimSuffix(line, "\r"))
	if t == "" || strings.HasPrefix(t, "#") {
		return "", "", false, true
	}
	i := strings.IndexByte(t, '=')
	if i <= 0 {
		return "", "", false, false
	}
	key = strings.ToLower(strings.TrimSpace(t[:i]))
	val = strings.TrimSpace(t[i+1:])
	if len(val) >= 2 && (val[0] == '"' && val[len(val)-1] == '"' || val[0] == '\'' && val[len(val)-1] == '\'') {
		val = val[1 : len(val)-1]
	} else if j := strings.Index(val, " #"); j >= 0 {
		val = strings.TrimSpace(val[:j]) // trailing comment
	}
	return key, val, key != "", false
}

// Get returns the value for key: the environment variable ASTROLABE_<KEY> if
// set and non-empty, else the file value.
func (c *Config) Get(key string) (string, bool) {
	key = strings.ToLower(key)
	if v := c.getenv("ASTROLABE_" + strings.ToUpper(key)); v != "" {
		return v, true
	}
	v, ok := c.values[key]
	return v, ok
}

// String returns the value for key or def.
func (c *Config) String(key, def string) string {
	if v, ok := c.Get(key); ok && v != "" {
		return v
	}
	return def
}

// Int returns the integer value for key, or def if unset or malformed.
func (c *Config) Int(key string, def int) int {
	if v, ok := c.Get(key); ok {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// Bool returns the boolean value for key (on/off, true/false, yes/no,
// 1/0), or def if unset or malformed.
func (c *Config) Bool(key string, def bool) bool {
	if v, ok := c.Get(key); ok {
		switch strings.ToLower(v) {
		case "on", "true", "yes", "1":
			return true
		case "off", "false", "no", "0":
			return false
		}
	}
	return def
}

// SetConfigValue persists one key in the config file (used when the theme
// is changed inside the TUI): an existing "key = …" line is replaced in
// place, otherwise the line is appended; comments, other lines and their
// order are kept. The file and its directory are created if needed and
// the write is atomic.
func SetConfigValue(file, key, value string) error {
	key = strings.ToLower(strings.TrimSpace(key))
	if key == "" || strings.ContainsAny(key, "=\n") || strings.ContainsAny(value, "\n\r") {
		return fmt.Errorf("invalid config entry %q", key)
	}
	data, err := os.ReadFile(file)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if strings.Contains(value, " #") || strings.TrimSpace(value) != value ||
		strings.HasPrefix(value, `"`) || strings.HasPrefix(value, "'") {
		value = `"` + value + `"` // read back verbatim by parseConfigLine
	}
	newLine := key + " = " + value
	lines := strings.SplitAfter(string(data), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	replaced := false
	for i, l := range lines {
		if k, _, ok, _ := parseConfigLine(l); ok && k == key {
			if replaced {
				lines[i] = "" // drop duplicates of the key
				continue
			}
			end := ""
			if strings.HasSuffix(l, "\r\n") {
				end = "\r\n"
			} else if strings.HasSuffix(l, "\n") {
				end = "\n"
			}
			lines[i] = newLine + end
			replaced = true
		}
	}
	out := strings.Join(lines, "")
	if !replaced {
		if out != "" && !strings.HasSuffix(out, "\n") {
			out += "\n"
		}
		out += newLine + "\n"
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	_, err = WriteFile(file, []byte(out), nil)
	return err
}

// FileValue returns the value of key as written in the config file,
// ignoring the ASTROLABE_<KEY> environment override (the remembered vault
// is config "dir", while $ASTROLABE_DIR is a separate, higher rule).
func (c *Config) FileValue(key string) (string, bool) {
	v, ok := c.values[strings.ToLower(key)]
	return v, ok
}

// UnsetConfigValue removes every "key = …" line from the config file,
// keeping comments and all other lines in their order. A missing file or
// key is not an error; removed reports whether a line was dropped.
func UnsetConfigValue(file, key string) (removed bool, err error) {
	key = strings.ToLower(strings.TrimSpace(key))
	data, err := os.ReadFile(file)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var kept []string
	for _, l := range strings.SplitAfter(string(data), "\n") {
		if k, _, ok, _ := parseConfigLine(l); ok && k == key {
			removed = true
			continue
		}
		kept = append(kept, l)
	}
	if !removed {
		return false, nil
	}
	_, err = WriteFile(file, []byte(strings.Join(kept, "")), nil)
	return err == nil, err
}

// ConfigWritable reports why the config file could not be written, or nil
// when it can be (for `astrolabe doctor`: remembering the vault fails soft).
// Nothing is changed: an existing file is opened for appending and closed,
// otherwise a probe file is created and removed in the nearest existing
// ancestor directory.
func ConfigWritable(file string) error {
	if f, err := os.OpenFile(file, os.O_WRONLY|os.O_APPEND, 0); err == nil {
		return f.Close()
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	dir := filepath.Dir(file)
	for {
		if fi, err := os.Stat(dir); err == nil {
			if !fi.IsDir() {
				return fmt.Errorf("%s is not a directory", dir)
			}
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	f, err := os.CreateTemp(dir, ".astrolabe-probe-*")
	if err != nil {
		return err
	}
	f.Close()
	return os.Remove(f.Name())
}

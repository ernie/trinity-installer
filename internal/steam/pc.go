package steam

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// LibraryFolders reads the "path" values of Steam's libraryfolders.vdf; the file is simple enough that a line scan beats a VDF parser.
func LibraryFolders(r io.Reader) []string {
	var paths []string
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		fields := strings.SplitN(strings.TrimSpace(sc.Text()), "\t", 2)
		if len(fields) != 2 || strings.Trim(fields[0], `"`) != "path" {
			continue
		}
		p := strings.Trim(strings.TrimSpace(fields[1]), `"`)
		paths = append(paths, strings.ReplaceAll(p, `\\`, `\`))
	}
	return paths
}

func SteamUserData(root string) (string, error) {
	entries, err := os.ReadDir(filepath.Join(root, "userdata"))
	if err != nil {
		return "", err
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() && e.Name() != "0" && e.Name() != "anonymous" {
			ids = append(ids, e.Name())
		}
	}
	if len(ids) == 0 {
		return "", fmt.Errorf("no Steam user found under %s", root)
	}
	if len(ids) != 1 {
		return "", fmt.Errorf("expected one Steam user, found %d: %s", len(ids), strings.Join(ids, ", "))
	}
	return filepath.Join(root, "userdata", ids[0]), nil
}

// SteamUser is the userdata folder of the PC's Steam user: the only one, or with several the one loginusers.vdf marks most recent.
func SteamUser(root string) (string, error) {
	dir, err := SteamUserData(root)
	if err == nil {
		return dir, nil
	}
	f, ferr := os.Open(filepath.Join(root, "config", "loginusers.vdf"))
	if ferr != nil {
		return "", err
	}
	defer f.Close()
	id64, ok := mostRecentUser(f)
	if !ok {
		return "", err
	}
	n, perr := strconv.ParseUint(id64, 10, 64)
	if perr != nil {
		return "", err
	}
	for _, name := range []string{strconv.FormatUint(n&0xFFFFFFFF, 10), id64} {
		dir := filepath.Join(root, "userdata", name)
		if st, serr := os.Stat(dir); serr == nil && st.IsDir() {
			return dir, nil
		}
	}
	return "", err
}

// mostRecentUser reads the id64 of the one user marked "MostRecent" "1" in loginusers.vdf's users block.
func mostRecentUser(r io.Reader) (string, bool) {
	doc, err := parseTextVDF(r)
	if err != nil {
		return "", false
	}
	var found []string
	for k, v := range doc {
		users, ok := v.(map[string]any)
		if !ok || !strings.EqualFold(k, "users") {
			continue
		}
		for id, e := range users {
			entry, _ := e.(map[string]any)
			for ek, ev := range entry {
				if strings.EqualFold(ek, "MostRecent") && ev == "1" {
					found = append(found, id)
				}
			}
		}
	}
	if len(found) != 1 {
		return "", false
	}
	return found[0], true
}

// parseTextVDF reads Valve's text key-value format: quoted keys followed by a quoted value or a braced block.
func parseTextVDF(r io.Reader) (map[string]any, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	toks, err := vdfTokens(string(b))
	if err != nil {
		return nil, err
	}
	m, rest, err := vdfBlock(toks)
	if err == nil && len(rest) > 0 {
		err = errors.New("unbalanced braces in VDF")
	}
	return m, err
}

func vdfBlock(toks []string) (map[string]any, []string, error) {
	m := map[string]any{}
	for len(toks) > 0 {
		key := toks[0]
		if key == "}" {
			return m, toks, nil
		}
		if key == "{" || len(toks) < 2 {
			return nil, nil, errors.New("malformed VDF")
		}
		if toks[1] != "{" {
			m[key[1:]] = toks[1][1:]
			toks = toks[2:]
			continue
		}
		child, rest, err := vdfBlock(toks[2:])
		if err != nil {
			return nil, nil, err
		}
		if len(rest) == 0 {
			return nil, nil, errors.New("unclosed VDF block")
		}
		m[key[1:]] = child
		toks = rest[1:]
	}
	return m, nil, nil
}

// vdfTokens returns "{", "}" and strings marked with a leading quote, so a string "{" cannot pass for a brace.
func vdfTokens(s string) ([]string, error) {
	var toks []string
	for i := 0; i < len(s); {
		switch c := s[i]; {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n':
			i++
		case c == '/' && strings.HasPrefix(s[i:], "//"):
			for i < len(s) && s[i] != '\n' {
				i++
			}
		case c == '{' || c == '}':
			toks = append(toks, string(c))
			i++
		case c == '"':
			var sb strings.Builder
			i++
			for ; i < len(s) && s[i] != '"'; i++ {
				if s[i] == '\\' && i+1 < len(s) {
					i++
				}
				sb.WriteByte(s[i])
			}
			if i >= len(s) {
				return nil, errors.New("unterminated VDF string")
			}
			i++
			toks = append(toks, `"`+sb.String())
		default:
			return nil, fmt.Errorf("unexpected %q in VDF", c)
		}
	}
	return toks, nil
}

// Libraries lists each root followed by the extra library folders its libraryfolders.vdf names.
func Libraries(roots []string) []string {
	var libs []string
	for _, root := range roots {
		libs = append(libs, root)
		if f, err := os.Open(filepath.Join(root, "steamapps", "libraryfolders.vdf")); err == nil {
			libs = append(libs, LibraryFolders(f)...)
			f.Close()
		}
	}
	return libs
}

// SteamVRRoot looks through every library folder for an installed SteamVR.
func SteamVRRoot(roots []string) (string, bool) {
	for _, lib := range Libraries(roots) {
		dir := filepath.Join(lib, "steamapps", "common", "SteamVR")
		if st, err := os.Stat(filepath.Join(dir, "bin")); err == nil && st.IsDir() {
			return dir, true
		}
	}
	return "", false
}

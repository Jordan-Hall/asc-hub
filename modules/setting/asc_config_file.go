// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package setting

import (
	"bufio"
	"os"
	"slices"
	"strings"
	"sync"

	"forgejo.org/modules/log"
)

// ConfigFileEntry is one key of the configuration file as it is written there: its section, its name, the raw value and the
// line it is on. Keys before the first section are in the section "DEFAULT", as the ini reader names it.
type ConfigFileEntry struct {
	Section, Key, Value string
	Line                int
}

// Name is "section.KEY", or KEY alone for the default section.
func (e ConfigFileEntry) Name() string {
	if e.Section == "DEFAULT" {
		return e.Key
	}
	return e.Section + "." + e.Key
}

// ConfigFile is the configuration file read again: where each key is written, keyed by Name in lower case.
type ConfigFile struct {
	Path    string
	Entries map[string]ConfigFileEntry
}

// Find is the first of the names ("section.KEY") that the file sets.
func (f *ConfigFile) Find(names ...string) (ConfigFileEntry, bool) {
	if f == nil {
		return ConfigFileEntry{}, false
	}
	for _, n := range names {
		if e, ok := f.Entries[strings.ToLower(n)]; ok {
			return e, true
		}
	}
	return ConfigFileEntry{}, false
}

// ReadConfigFile reads the keys of an ini file with their lines. It reads the file the way the ini reader does for what
// it needs here: "[section]" headers, "KEY = value" and "KEY: value" lines, ";" and "#" comments. The value is kept as
// written, so two reads can be compared.
func ReadConfigFile(path string) (*ConfigFile, error) {
	fh, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	f := &ConfigFile{Path: path, Entries: map[string]ConfigFileEntry{}}
	section := "DEFAULT"
	sc := bufio.NewScanner(fh)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if n == 1 {
			line = strings.TrimPrefix(line, "\ufeff")
		}
		if line == "" || line[0] == ';' || line[0] == '#' {
			continue
		}
		if line[0] == '[' {
			if end := strings.IndexByte(line, ']'); end > 0 {
				section = strings.TrimSpace(line[1:end])
			}
			continue
		}
		at := strings.IndexAny(line, "=:")
		if at <= 0 {
			continue
		}
		e := ConfigFileEntry{Section: section, Key: strings.TrimSpace(line[:at]), Value: strings.TrimSpace(line[at+1:]), Line: n}
		f.Entries[strings.ToLower(e.Name())] = e
	}
	return f, sc.Err()
}

var bootConfigFile struct {
	sync.Mutex
	file *ConfigFile
}

// RememberConfigFile keeps the configuration file as it was when the server started, so the configuration page can say
// which keys changed since and need a restart. It is called once the settings are loaded.
func RememberConfigFile() {
	f, err := ReadConfigFile(CustomConf)
	if err != nil {
		log.Warn("the configuration file %q could not be read to compare it later: %v", CustomConf, err)
		f = nil
	}
	bootConfigFile.Lock()
	bootConfigFile.file = f
	bootConfigFile.Unlock()
}

// ConfigFileNow reads the configuration file again, and lists the keys ("section.KEY") that were added, changed or
// removed since the server started, in the order of the file (removed keys last). With no file read at start, nothing
// counts as changed.
func ConfigFileNow() (*ConfigFile, []string, error) {
	now, err := ReadConfigFile(CustomConf)
	if err != nil {
		return nil, nil, err
	}
	bootConfigFile.Lock()
	boot := bootConfigFile.file
	bootConfigFile.Unlock()
	return now, ChangedConfigKeys(boot, now), nil
}

// ChangedConfigKeys lists the keys whose value differs between two reads of the configuration file.
func ChangedConfigKeys(before, after *ConfigFile) []string {
	if before == nil || after == nil {
		return nil
	}
	var changed, removed []ConfigFileEntry
	for k, e := range after.Entries {
		if b, ok := before.Entries[k]; !ok || b.Value != e.Value {
			changed = append(changed, e)
		}
	}
	for k, e := range before.Entries {
		if _, ok := after.Entries[k]; !ok {
			removed = append(removed, e)
		}
	}
	byLine := func(a, b ConfigFileEntry) int { return a.Line - b.Line }
	slices.SortFunc(changed, byLine)
	slices.SortFunc(removed, byLine)
	names := make([]string, 0, len(changed)+len(removed))
	for _, e := range append(changed, removed...) {
		names = append(names, e.Name())
	}
	return names
}

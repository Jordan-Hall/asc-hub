// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repostore

import (
	"fmt"
	"sort"
	"sync"
)

// DefaultName names the Writer that Default returns.
const DefaultName = "gitcli"

var (
	mu      sync.RWMutex
	writers = map[string]Writer{}
)

// Register registers a Writer under name. It panics on a nil Writer or a
// duplicate name, because both are programming errors found at start-up.
func Register(name string, w Writer) {
	if w == nil {
		panic("repostore: Register with nil Writer")
	}
	mu.Lock()
	defer mu.Unlock()
	if _, dup := writers[name]; dup {
		panic("repostore: Register called twice for " + name)
	}
	writers[name] = w
}

// Names returns the registered Writer names, sorted.
func Names() []string {
	mu.RLock()
	defer mu.RUnlock()
	names := make([]string, 0, len(writers))
	for name := range writers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Get returns the Writer registered under name.
func Get(name string) (Writer, error) {
	mu.RLock()
	defer mu.RUnlock()
	w, ok := writers[name]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrNoWriter, name)
	}
	return w, nil
}

// Default returns the Writer the services use. It fails closed when none is linked in.
func Default() (Writer, error) {
	return Get(DefaultName)
}

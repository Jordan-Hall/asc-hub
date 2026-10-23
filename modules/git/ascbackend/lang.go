// Copyright 2020 The Gitea Authors. All rights reserved.
// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package ascbackend

import (
	"cmp"
	"context"
	"io"
	"strings"
	"unicode"

	"forgejo.org/modules/analyze"
	"forgejo.org/modules/git"
	"forgejo.org/modules/log"
	"forgejo.org/modules/optional"

	"github.com/go-enry/go-enry/v2"
	"golang.org/x/sync/errgroup"
)

const (
	languageSampleSize int64 = 16 * 1024   // bytes of a file read to detect its language
	languageBigFile    int64 = 1024 * 1024 // a bigger file is detected by name only
)

// mergeLanguageStats merges language names with different cases. The name with most upper case letters is used.
func mergeLanguageStats(stats map[string]int64) map[string]int64 {
	type best struct {
		name  string
		upper int
	}
	names := map[string]best{}
	for name := range stats {
		upper := 0
		for _, r := range name {
			if unicode.IsUpper(r) {
				upper++
			}
		}
		lower := strings.ToLower(name)
		if upper >= names[lower].upper {
			names[lower] = best{name: name, upper: upper}
		}
	}
	res := make(map[string]int64, len(names))
	for name, n := range stats {
		res[names[strings.ToLower(name)].name] += n
	}
	return res
}

type langAction int

const (
	langSkip   langAction = iota // the file is not counted
	langAttr                     // the language is set by an attribute
	langDetect                   // detect the language, from the content when it was read
)

// langFile is what the first pass learns about one file.
type langFile struct {
	entry        git.TreeEntryData
	action       langAction
	attrLanguage string
	wantContent  bool
	content      []byte
	isGenerated  optional.Option[bool]
	isDocument   optional.Option[bool]
	isDetectable optional.Option[bool]
}

func isTrue(v optional.Option[bool]) bool  { return v.ValueOrDefault(false) }
func isFalse(v optional.Option[bool]) bool { return !v.ValueOrDefault(true) }

// LanguageStats measures the languages of the tree at, computed here over the listing, the attributes
// and the start of each file, with the same rules as git.Repository.GetLanguageStats.
func (b *Backend) LanguageStats(ctx context.Context, at git.ObjectID) (map[string]int64, error) {
	if err := requireSha1(at); err != nil {
		return nil, err
	}
	var files []*langFile
	for e, err := range b.ReadTreeRecursive(ctx, at, git.TreeWalkOpts{Sizes: true}) {
		if err != nil {
			return nil, err
		}
		if e.Size > 0 {
			files = append(files, &langFile{entry: e})
		}
	}

	if err := b.classifyLanguageFiles(ctx, at, files); err != nil {
		return nil, err
	}
	if err := b.loadLanguageContent(ctx, files); err != nil {
		return nil, err
	}

	sizes := make(map[string]int64)
	includedLanguage := map[string]bool{}
	firstExcludedLanguage := ""
	firstExcludedLanguageSize := int64(0)

	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		name := f.entry.Name
		switch f.action {
		case langSkip:
			continue
		case langAttr:
			sizes[f.attrLanguage] += f.entry.Size
			continue
		}

		// linguist-generated=true ignores the file, =false keeps it, otherwise enry decides.
		if isTrue(f.isGenerated) || !isFalse(f.isGenerated) && enry.IsGenerated(name, f.content) {
			log.Trace("Ignore %q for language stats, because it is generated", name)
			continue
		}
		language := analyze.GetCodeLanguage(name, f.content)
		if language == "" {
			continue
		}
		if group := enry.GetLanguageGroup(language); group != "" {
			language = group
		}
		included, checked := includedLanguage[language]
		langType := enry.GetLanguageType(language)
		if !checked {
			included = langType == enry.Programming || langType == enry.Markup
			if !included && (isTrue(f.isDetectable) || (langType == enry.Prose && isFalse(f.isDocument))) {
				included = true
			}
			includedLanguage[language] = included
		}
		if included {
			sizes[language] += f.entry.Size
		} else if len(sizes) == 0 && (firstExcludedLanguage == "" || firstExcludedLanguage == language) {
			// Only consider Programming or Markup languages as fallback
			if langType != enry.Programming && langType != enry.Markup {
				continue
			}
			firstExcludedLanguage = language
			firstExcludedLanguageSize += f.entry.Size
		}
	}
	// If there are no included languages add the first excluded language
	if len(sizes) == 0 && firstExcludedLanguage != "" {
		sizes[firstExcludedLanguage] = firstExcludedLanguageSize
	}
	return mergeLanguageStats(sizes), nil
}

// classifyLanguageFiles reads the linguist attributes of every file and decides what to do with it.
func (b *Backend) classifyLanguageFiles(ctx context.Context, at git.ObjectID, files []*langFile) error {
	contentReads := 0
	for start := 0; start < len(files); start += maxAttributePaths {
		chunk := files[start:min(start+maxAttributePaths, len(files))]
		paths := make([]string, len(chunk))
		for i, f := range chunk {
			paths[i] = f.entry.Name
		}
		attrs, err := b.Attributes(ctx, at, paths, git.LinguistAttributes)
		if err != nil {
			return err
		}
		for _, f := range chunk {
			a := func(name string) git.GitAttribute { return git.GitAttribute(attrs[f.entry.Name][name]) }
			isVendored := a("linguist-vendored").Bool()
			f.isGenerated = a("linguist-generated").Bool()
			f.isDocument = a("linguist-documentation").Bool()
			f.isDetectable = a("linguist-detectable").Bool()
			if language := cmp.Or(a("linguist-language").String(), a("gitlab-language").Prefix()); language != "" {
				// group languages, such as Pug -> HTML; SCSS -> CSS
				if group := enry.GetLanguageGroup(language); len(group) != 0 {
					language = group
				}
				f.action, f.attrLanguage = langAttr, language
				continue
			}
			// Skip a file that is explicitly not detectable, vendored or documentation, or that
			// heuristics call vendored, a dot file, configuration or documentation, unless it is
			// explicitly detectable.
			if !isTrue(f.isDetectable) &&
				(isFalse(f.isDetectable) || isTrue(isVendored) || isTrue(f.isDocument) ||
					(!isFalse(isVendored) && analyze.IsVendor(f.entry.Name)) ||
					enry.IsDotFile(f.entry.Name) ||
					enry.IsConfiguration(f.entry.Name) ||
					(!isFalse(f.isDocument) && enry.IsDocumentation(f.entry.Name))) {
				f.action = langSkip
				continue
			}
			f.action = langDetect
			if f.entry.Size <= languageBigFile {
				if contentReads++; contentReads <= maxLanguageContentReads {
					f.wantContent = true
				} else if contentReads == maxLanguageContentReads+1 {
					log.Warn("asc language stats of %s: more than %d files, the rest are detected by name", b.name, maxLanguageContentReads)
				}
			}
		}
	}
	return nil
}

// loadLanguageContent reads the start of every file that needs it, a few at a time.
func (b *Backend) loadLanguageContent(ctx context.Context, files []*langFile) error {
	var g errgroup.Group
	g.SetLimit(languageReadWorkers)
	for _, f := range files {
		if !f.wantContent {
			continue
		}
		g.Go(func() error {
			rc, _, err := b.OpenBlob(ctx, f.entry.ID, languageSampleSize)
			if err != nil {
				return err
			}
			defer rc.Close()
			f.content, err = io.ReadAll(rc)
			return err
		})
	}
	return g.Wait()
}

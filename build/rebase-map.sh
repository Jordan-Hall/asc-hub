#!/usr/bin/env bash
# Copyright 2026 The ASC Authors. All rights reserved.
# SPDX-License-Identifier: MIT
#
# rebase-map.sh - list upstream changes to modules/git/X.go against modules/git/gitcli/Y.go.
#
# K03 moved the git-binary code of modules/git into modules/git/gitcli (SPEC section 13.3). When
# upstream Forgejo changes a function that moved, `git rebase` cannot carry the change over: the
# hunk lands in a file that no longer holds the function. This script lists, for an upstream range,
# every changed non-test file of modules/git, where its moved code lives now, and which hunks touch
# moved code, so the change can be ported by hand.
#
# Usage: build/rebase-map.sh [-h] OLD [NEW]
#   OLD  the upstream revision the fork is based on (a tag or commit)
#   NEW  the upstream revision to rebase onto (default: HEAD)
#
# Exit status: 0 when nothing needs porting, 1 when at least one moved or mirrored function changed,
# 2 on a usage error.
#
# Table format: upstream file | gitcli file | kind | function names (extended regex alternation).
#   moved    the code left the upstream file; port the upstream change into the gitcli file.
#   mirrored the gitcli file holds a copy and the upstream function still exists; apply the change to both.
set -euo pipefail

usage() {
	sed -n '5,/^set -euo/p' "$0" | sed '$d' | sed 's/^# \{0,1\}//'
}

case "${1:-}" in
-h | --help)
	usage
	exit 0
	;;
esac

if [ "$#" -lt 1 ] || [ "$#" -gt 2 ]; then
	usage >&2
	exit 2
fi

# A revision is data, never an option: --end-of-options keeps a value such as "--output=x" from being read as one.
resolve() {
	git rev-parse --verify --quiet --end-of-options "$1^{commit}" || {
		echo "rebase-map: not a commit: $1" >&2
		exit 2
	}
}
old=$(resolve "$1")
new=$(resolve "${2:-HEAD}")

readonly MAP='
tree.go|objects.go|moved|ListEntries|listEntriesRecursive|ListEntriesRecursive
tree_entry.go|objects.go|moved|Size
blob.go|objects.go|moved|newReader|blobReader|DataAsync|NewReader|NewTruncatedReader|Size
repo_commit.go|objects.go|moved|getCommit|getCommitFromBatchReader|ConvertToGitID
repo_tag.go|objects.go|moved|GetTagType|getTag
parse.go|objects.go|mirrored|parseTreeEntries
repo_ref.go|refs.go|moved|GetRefsFiltered
repo_branch.go|refs.go|moved|GetHEADBranch
repo.go|history.go|moved|GetAllCommitsCount
commit.go|history.go|moved|CommitsCount
repo_compare.go|diff.go|moved|GetFilesChangedBetween
repo_compare.go|diff.go|mirrored|GetDiff|GetDiffBinary|GetPatch|GetDiffFromMergeBase|GetShortStat|parseDiffStat
repo_archive.go|bulk.go|moved|CreateArchive
grep.go|bulk.go|moved|GrepSearch
'

status=0
changed=0

# -z keeps unusual file names intact and prints "<status> NUL <path> NUL" pairs.
# Test files and sub-packages are not part of the map.
while IFS= read -r -d '' code && IFS= read -r -d '' path; do
	case "$path" in
	modules/git/*.go) ;;
	*) continue ;;
	esac
	file=${path#modules/git/}
	case "$file" in
	*/*) continue ;; # sub-packages (gitcli, pipeline, foreachref, ...) are not part of the map
	*_test.go) continue ;;
	esac
	changed=$((changed + 1))

	listed=0
	while IFS='|' read -r up dest kind funcs; do
		[ "$up" = "$file" ] || continue
		listed=1
		# funcs is the rest of the table line: alternatives of one extended regular expression.
		pattern=$funcs
		echo "$code  $path -> modules/git/gitcli/$dest  [$kind]"
		# `@@ -a,b +c,d @@ <function context>`: the context names the enclosing function.
		hits=0
		while IFS= read -r hunk; do
			context=${hunk#@@*@@}
			context=${context# }
			if printf '%s\n' "$context" | grep -Eq "(^|[^A-Za-z0-9_])($pattern)([^A-Za-z0-9_]|\$)"; then
				echo "    port: $hunk"
				hits=$((hits + 1))
			fi
		done < <(git diff -U0 --no-color --no-ext-diff "$old" "$new" -- "$path" | grep '^@@' || true)
		if [ "$hits" -gt 0 ]; then
			status=1
		elif [ "$code" = "D" ]; then
			# A deleted upstream file leaves the moved code without an upstream home.
			echo "    port: file deleted upstream"
			status=1
		else
			echo "    no hunk touches $kind code"
		fi
	done <<<"$MAP"

	if [ "$listed" -eq 0 ]; then
		echo "$code  $path  [stays in modules/git]"
	fi
done < <(git diff --name-status -z --no-renames --no-ext-diff "$old" "$new" -- modules/git)

if [ "$changed" -eq 0 ]; then
	echo "no changes to modules/git between $old and $new"
fi
exit "$status"

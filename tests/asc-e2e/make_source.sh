#!/bin/bash
# a git repository with two tags and an LFS pointer, served over dumb http by `python3 -m http.server 8099` from /tmp
# usage: make_source.sh <lfs oid> <lfs size>
export GIT_AUTHOR_NAME=T GIT_AUTHOR_EMAIL=t@e GIT_COMMITTER_NAME=T GIT_COMMITTER_EMAIL=t@e
rm -rf /tmp/w-rel /tmp/src-rel.git; mkdir /tmp/w-rel && cd /tmp/w-rel && git init -q -b master .
echo "# proj" > README.md; git add -A; git commit -q -m one; git tag v1.0
printf 'version https://git-lfs.github.com/spec/v1\noid sha256:%s\nsize %s\n' "$1" "$2" > big.bin
echo "big.bin filter=lfs diff=lfs merge=lfs -text" > .gitattributes
git add -A; git commit -q -m "two with an lfs pointer"; git tag v2.0
git clone -q --bare /tmp/w-rel /tmp/src-rel.git; (cd /tmp/src-rel.git && git update-server-info); chmod -R a+rX /tmp/src-rel.git; echo ok

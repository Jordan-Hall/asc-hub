// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package ascbackend

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"forgejo.org/modules/git"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const maxRepoNameBytes = 255

// RepoName derives the asc repository name ("owner/repo", lower case as on disk) from the path of the
// repository directory below root. A wiki repository, a path outside root and any name with a
// character Forgejo does not allow in owner and repository names are refused.
func RepoName(root, path string) (string, error) {
	if root == "" {
		return "", errors.New("ascbackend: the repository root is not set")
	}
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	if err != nil {
		return "", errors.New("ascbackend: the repository is not below the repository root")
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) != 2 || !strings.HasSuffix(parts[1], ".git") {
		return "", errors.New("ascbackend: the repository path is not owner/name.git below the repository root")
	}
	name := parts[0] + "/" + strings.TrimSuffix(parts[1], ".git")
	if err := validRepoName(name); err != nil {
		return "", err
	}
	return name, nil
}

func validRepoNamePart(s string) bool {
	if s == "" || s == "." || s == ".." || strings.HasSuffix(s, ".wiki") {
		return false
	}
	for i := range len(s) {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_', c == '.':
		default:
			return false
		}
	}
	return true
}

// validRepoName accepts "owner/repo" and nothing else.
func validRepoName(name string) error {
	owner, repo, ok := strings.Cut(name, "/")
	if !ok || len(name) > maxRepoNameBytes || !validRepoNamePart(owner) || !validRepoNamePart(repo) {
		return errors.New("ascbackend: not a valid asc repository name")
	}
	return nil
}

// idFromWire turns the 20 bytes of a git id into an ObjectID. Any other length is a bad answer.
func idFromWire(b []byte) (git.ObjectID, error) {
	if len(b) != git.Sha1ObjectFormat.FullLength()/2 {
		return nil, fmt.Errorf("%w: object id of %d bytes", ErrBadAnswer, len(b))
	}
	return git.Sha1ObjectFormat.MustID(b), nil
}

// idsFromWire converts a list of git ids.
func idsFromWire(list [][]byte) ([]git.ObjectID, error) {
	out := make([]git.ObjectID, len(list))
	for i, b := range list {
		id, err := idFromWire(b)
		if err != nil {
			return nil, err
		}
		out[i] = id
	}
	return out, nil
}

// requireSha1 refuses an id of another object format.
func requireSha1(id git.ObjectID) error {
	if id == nil || id.Type() != git.Sha1ObjectFormat {
		return errors.New("ascbackend: only sha1 object ids are supported")
	}
	return nil
}

// validRev reports whether rev can be sent as a revision.
func validRev(rev string) bool {
	return rev != "" && len(rev) <= maxRevBytes && !strings.ContainsAny(rev, "\n\x00") && utf8.ValidString(rev)
}

// validEntryName checks one tree entry name received from the server.
func validEntryName(name []byte) bool {
	if len(name) == 0 || len(name) > maxEntryNameBytes {
		return false
	}
	for _, c := range name {
		if c == 0 || c == '/' {
			return false
		}
	}
	s := string(name)
	return s != "." && s != ".."
}

// Error is a failed asc call. It never carries a credential: the message is the server's status text,
// reduced to printable ASCII and cut short.
type Error struct {
	Op   string
	Code codes.Code
	Msg  string
}

func (e *Error) Error() string {
	if e.Msg == "" {
		return fmt.Sprintf("ascbackend: %s: asc server answered %s", e.Op, e.Code)
	}
	return fmt.Sprintf("ascbackend: %s: asc server answered %s: %s", e.Op, e.Code, e.Msg)
}

// Is maps status codes onto the package errors.
func (e *Error) Is(target error) bool {
	switch target {
	case ErrUnsupported:
		return e.Code == codes.Unimplemented
	case ErrTooLarge:
		return e.Code == codes.ResourceExhausted || e.Code == codes.OutOfRange
	}
	return false
}

func sanitize(s string) string {
	var b strings.Builder
	for _, r := range s {
		if b.Len() >= maxMessageEcho {
			b.WriteString("...")
			break
		}
		if r >= 0x20 && r < 0x7f {
			b.WriteRune(r)
		} else {
			b.WriteByte('?')
		}
	}
	return b.String()
}

// callErr turns a failed call into an error of this package. A missing object becomes
// git.ErrNotExist{ID: id}; context errors pass through unchanged; everything else is an *Error.
func callErr(op, id string, err error) error {
	if err == nil {
		return nil
	}
	st, ok := status.FromError(err)
	if !ok {
		return err // a local error: context, closed client, missing credential
	}
	switch st.Code() {
	case codes.NotFound:
		if st.Message() == repoNotFoundMessage {
			break // the repository, not an object in it: a hard error
		}
		return git.ErrNotExist{ID: id}
	case codes.Canceled, codes.DeadlineExceeded:
		return err
	}
	return &Error{Op: op, Code: st.Code(), Msg: sanitize(st.Message())}
}

// repoNotFoundMessage is what asc serve says when it has no such repository.
const repoNotFoundMessage = "no such repository"

// isMissing reports whether err says that an object (not the repository) does not exist.
func isMissing(err error) bool {
	st, ok := status.FromError(err)
	return ok && st.Code() == codes.NotFound && st.Message() != repoNotFoundMessage
}

// isCode reports whether err is a gRPC status with the given code.
func isCode(err error, code codes.Code) bool {
	st, ok := status.FromError(err)
	return ok && st.Code() == code
}

func badAnswer(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrBadAnswer, fmt.Sprintf(format, args...))
}

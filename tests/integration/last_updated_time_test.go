package integration

import (
	"net/http"
	"path"
	"strings"
	"testing"

	"forgejo.org/tests"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/assert"
)

func TestRepoLastUpdatedTime(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	user := "user2"
	session := loginUser(t, user)

	req := NewRequest(t, "GET", "/explore/repos?q=repo1")
	resp := session.MakeRequest(t, req, http.StatusOK)
	doc := NewHTMLParser(t, resp.Body)
	node := doc.doc.Find(".ds-repolist > li:has(a[href='/user2/repo1']) .ds-li-meta > span:has(relative-time)").First()
	{
		buf := ""
		findTextNonNested(t, node, &buf)
		assert.Equal(t, "Updated", strings.TrimSpace(buf))
	}

	// Relative time should be present as a descendent
	assert.Contains(t, node.Find("relative-time").Text(), "2024-11-10")
}

func TestBranchLastUpdatedTime(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	user := "user2"
	repo := "repo1"
	session := loginUser(t, user)

	req := NewRequest(t, "GET", path.Join(user, repo, "branches"))
	resp := session.MakeRequest(t, req, http.StatusOK)
	doc := NewHTMLParser(t, resp.Body)
	node := doc.doc.Find(".rp-ref-meta > span:has(relative-time)")

	{
		buf := ""
		findTextNonNested(t, node, &buf)
		assert.Contains(t, buf, "Updated")
	}

	{
		relativeTime := node.Find("relative-time").Text()
		assert.True(t, strings.HasPrefix(relativeTime, "2017"))
	}
}

// Find all text that are direct descendents
func findTextNonNested(t *testing.T, n *goquery.Selection, buf *string) {
	t.Helper()

	n.Contents().Each(func(i int, s *goquery.Selection) {
		if goquery.NodeName(s) == "#text" {
			*buf += s.Text()
		}
	})
}

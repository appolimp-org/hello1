package integrationtests

import (
	"common/testutils/yarequire"
	"fmt"
	"gitcore/internal/entities"
	"net/http"

	"github.com/stretchr/testify/require"
)

func (suite *RwApiTestSuite) TestPublicAPINoAnons() {
	t := suite.T()

	repoID, orgSlug, repoSlug := suite.makeRandomRepo(t, suite.users.Admin) // public
	suite.makeIssue(suite.users.Krosh, &makeIssueOptions{
		RepoID:     repoID,
		Title:      "Public",
		Visibility: entities.IssueVisibilities.Public, // public
	})

	resp, err := suite.gwClient.AsGuest().Get(fmt.Sprintf("/repos/%s/%s/issues", orgSlug, repoSlug))
	yarequire.StatusCode(t, resp, err, http.StatusUnauthorized)
}

func (suite *RwApiTestSuite) TestMalformedJSONWrongField() {
	t := suite.T()

	repoID, orgSlug, repoSlug := suite.makeRandomRepo(t, suite.users.Admin)

	issue := suite.makeIssue(suite.users.Kopatych, &makeIssueOptions{
		ID:         2000,
		RepoID:     repoID,
		Title:      "Issue0",
		Visibility: entities.IssueVisibilities.Public,
		LabelIDs:   []uint64{},
	})

	resp, err := suite.gwClient.
		As(suite.users.Kopatych.Identity).
		SetBody(map[string]string{"slugs": "aaaa"}).
		Post(fmt.Sprintf("/repos/%s/%s/issues/%d/labels", orgSlug, repoSlug, issue.PublicID))

	yarequire.StatusCode(t, resp, err, 400)
	require.NoError(t, err)
}

func (suite *RwApiTestSuite) TestPublicAPIDocs() {
	t := suite.T()
	resp, err := suite.gwClient.AsGuest().Get("/sourcecraft.swagger.json")
	yarequire.StatusCode(t, resp, err, http.StatusOK)

}

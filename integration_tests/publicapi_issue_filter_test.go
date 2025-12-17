package integrationtests

import (
	"common/testutils/yarequire"
	"fmt"
	"gitcore/internal/entities"
	"testing"

	"github.com/go-resty/resty/v2"

	"github.com/stretchr/testify/require"
)

func (suite *RwApiTestSuite) TestPublicAPIListIssueFilterTest() {
	f := prepareIssuesForFiltering(suite)

	cases := map[string]struct {
		user   *entities.User
		filter string
	}{
		"filter by issue title": {
			filter: `title="%issue"`,
			// Matches: 1, 3
		},
		"filter by priority": {
			filter: `priority=critical`,
			// Matches: 1, 4
		},
		"filter by status": {
			filter: `status=open`,
			// Matches: 1, 3
		},
		"complex filter": {
			filter: `status=open and priority=critical`,
			// Matches: 1
		},
	}

	for tn, tc := range cases {
		suite.T().Run(tn, func(t *testing.T) {
			resp, err := suite.gwClient.
				As(suite.users.Admin.Identity).
				SetQueryParam("filter", tc.filter).
				Get(fmt.Sprintf("/repos/%s/%s/issues", f.orgSlug, f.repoSlug))
			yarequire.StatusCode(t, resp, err, 200)

			// yarequire.HTTPDumpFixture(t, resp)
			yarequire.HTTPCompareWithFixture(t, resp,
				"**/updated_at", "**/created_at", "**/id",
				"**/started_at", "**/completed_at")
		})
	}

	casesWithMultipleRequests := map[string]struct {
		filters []string
	}{
		"filter by author with equal": {
			filters: []string{
				fmt.Sprintf("author_id=\"%d\"", suite.users.Pikachu.ID),
				fmt.Sprintf("author_uuid=\"%s\"", suite.users.Pikachu.UUID.String()),
				fmt.Sprintf("author_slug=\"%s\"", suite.users.Pikachu.Username),
			},
			// Matches: 3, 5
		},
		"filter by author with not equal": {
			filters: []string{
				fmt.Sprintf("author_id!=\"%d\"", suite.users.Pikachu.ID),
				fmt.Sprintf("author_uuid!=\"%s\"", suite.users.Pikachu.UUID.String()),
				fmt.Sprintf("author_slug!=\"%s\"", suite.users.Pikachu.Username),
			},
			// Matches: 1, 2, 4
		},
		"filter by assignee with equal": {
			filters: []string{
				fmt.Sprintf("assignee_id=\"%d\"", suite.users.Pikachu.ID),
				fmt.Sprintf("assignee_uuid=\"%s\"", suite.users.Pikachu.UUID.String()),
				fmt.Sprintf("assignee_slug=\"%s\"", suite.users.Pikachu.Username),
			},
			// Matches: 2, 5
		},
		"filter by assignee with not equal": {
			filters: []string{

				fmt.Sprintf("assignee_id!=\"%d\"", suite.users.Pikachu.ID),
				fmt.Sprintf("assignee_uuid!=\"%s\"", suite.users.Pikachu.UUID.String()),
				fmt.Sprintf("assignee_slug!=\"%s\"", suite.users.Pikachu.Username),
			},
			// Matches: 1, 3, 4
		},
		"filter by milestone with equal": {
			filters: []string{
				fmt.Sprintf("milestone_id=\"%d\"", f.milestone1.ID),
				fmt.Sprintf("milestone_uuid=\"%s\"", f.milestone1.UUID.String()),
				fmt.Sprintf("milestone_slug=\"%s\"", *f.milestone1.Slug),
			},
			// Matches: 3, 4
		},
		"filter by milestone with not equal": {
			filters: []string{
				fmt.Sprintf("milestone_id!=\"%d\"", f.milestone1.ID),
				fmt.Sprintf("milestone_uuid!=\"%s\"", f.milestone1.UUID.String()),
				fmt.Sprintf("milestone_slug!=\"%s\"", *f.milestone1.Slug),
			},
			// Matches: 1, 2, 5
		},
		"filter by label with equal": {
			filters: []string{
				fmt.Sprintf("label_id=\"%d\"", f.label1.ID),
				fmt.Sprintf("label_ids=\"%d\"", f.label1.ID), //deprecated
				fmt.Sprintf("label_uuid=\"%s\"", f.label1.UUID.String()),
				fmt.Sprintf("label_slug=\"%s\"", *f.label1.Slug),
			},
			// Matches: 1, 3, 4
		},
		"filter by label with not equal": {
			filters: []string{
				fmt.Sprintf("label_id!=\"%d\"", f.label1.ID),
				fmt.Sprintf("label_ids!=\"%d\"", f.label1.ID), // deprecated
				fmt.Sprintf("label_uuid!=\"%s\"", f.label1.UUID.String()),
				fmt.Sprintf("label_slug!=\"%s\"", *f.label1.Slug),
			},
			// Matches: 2, 5
		},
	}
	for tn, tc := range casesWithMultipleRequests {
		suite.T().Run(tn, func(t *testing.T) {
			var responses []*resty.Response
			for _, filter := range tc.filters {
				resp, err := suite.gwClient.
					As(suite.users.Admin.Identity).
					SetQueryParam("filter", filter).
					Get(fmt.Sprintf("/repos/%s/%s/issues", f.orgSlug, f.repoSlug))
				yarequire.StatusCode(t, resp, err, 200)
				responses = append(responses, resp)
			}

			// check that all responses are equal
			require.Greater(t, len(responses), 0)
			// yarequire.HTTPDumpFixture(t, responses[0])
			for _, resp := range responses {
				yarequire.HTTPCompareWithFixture(t, resp,
					"**/updated_at", "**/created_at", "**/id",
					"**/started_at", "**/completed_at")
			}
		})
	}
}

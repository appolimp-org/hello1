package integrationtests

import (
	"common/testutils/yarequire"
	"fmt"
	"net/http"
	pbPub "private_api/generated/yandex/cloud/public/sourcecraft/v1"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func (suite *RwApiTestSuite) TestPublicAPIPullRequestCommentsCrud() {
	t := suite.T()

	// Use existing test repository fixture instead of creating new ones
	repo := suite.repos.Alpha
	orgSlug := suite.orgs.Yandex.Slug
	repoSlug := repo.Slug

	// Setup repository with actual file changes first
	suite.mustBash(suite.repos.Alpha, `
		git checkout master
		echo 'import (
	"fmt"
)

func main() {
	s := "Hello, World!"
	fmt.Println(s)
}' > newfile.go
		git add . && git commit -m "Add initial Go file"
		
		git checkout branch
		echo 'import (
	"fmt"
	"math"
)

func main() {
	s := "Hello, John!"
	num := 9
	fmt.Println(s)
	sqrt := math.Sqrt(num)
	fmt.Println(sqrt)
}' > newfile.go
		git add . && git commit -m "Update Go file"
	`)

	// Create a pull request for testing comments using default branches
	pr := suite.makePullRequest(suite.users.Admin, &makePrOptions{
		Title:       "Test PR for comments",
		Description: "PR to test comments functionality",
	})

	// Create another PR for testing by ID operations
	prForIDOps := suite.makePullRequest(suite.users.Admin, &makePrOptions{
		Title:       "Test PR for ID operations",
		Description: "PR to test ID-based operations",
	})

	// Prepopulate some comments for testing list operations
	rootComment := suite.makePrCommentGRPC(t, suite.users.Krosh, pr, &makePrCommentOptions{
		Body: "Root comment for testing",
	})

	// Create a child comment (reply)
	suite.makePrCommentGRPC(t, suite.users.Kopatych, pr, &makePrCommentOptions{
		ParentID: &rootComment.ID,
		Body:     "Reply to root comment",
	})

	// Create a draft comment for testing drafts functionality
	suite.makePrCommentGRPC(t, suite.users.Krosh, pr, &makePrCommentOptions{
		Body:  "Draft comment",
		Draft: true,
	})

	// Create comments for the prForIDOps PR so the "list by id" test has data
	suite.makePrCommentGRPC(t, suite.users.Krosh, prForIDOps, &makePrCommentOptions{
		Body: "Comment for ID-based operations",
	})

	t.Run("comment with anchor", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Admin.Identity).
			SetBody(&pbPub.CreatePullRequestCommentBody{
				Body:      "Comment on code change",
				Iteration: strconv.FormatUint(pr.Iteration, 10),
				Anchor: &pbPub.CreatePullRequestCommentBody_ShortAnchor{
					Path: "newfile.go",
					Position: &pbPub.DiffPos{
						From: 6,
						To:   6,
						Side: pbPub.DiffPos_source,
					},
				},
			}).
			Post(fmt.Sprintf("/repos/%s/%s/pulls/%d/comments", orgSlug, repoSlug, pr.PublicID))

		require.NoError(t, err)
		yarequire.StatusCode(t, resp, err, http.StatusCreated)
		yarequire.HTTPCompareWithFixture(t, resp, "**/updated_at", "**/created_at", "**/id", "**/iteration", "**/parent_id")
	})

	t.Run("list pull request comments", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Kopatych.Identity).
			Get(fmt.Sprintf("/repos/%s/%s/pulls/%d/comments", orgSlug, repoSlug, pr.PublicID))
		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, resp, "**/updated_at", "**/created_at", "**/id", "**/iteration", "**/parent_id")
	})

	t.Run("list pull request comments by id", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Kopatych.Identity).
			Get(fmt.Sprintf("/pulls/id:%s/comments", prForIDOps.UUID.String()))
		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, resp, "**/updated_at", "**/created_at", "**/id", "**/iteration")
	})

	t.Run("list pull request comments with drafts", func(t *testing.T) {
		resp, err := suite.gwClient.As(suite.users.Krosh.Identity).SetQueryParam("only_drafts", "true").
			Get(fmt.Sprintf("/repos/%s/%s/pulls/%d/comments", orgSlug, repoSlug, pr.PublicID))
		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, resp, "**/updated_at", "**/created_at", "**/id", "**/iteration")
	})

	t.Run("list pull request comments with pagination", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Kopatych.Identity).
			SetQueryParam("page_size", "2").
			Get(fmt.Sprintf("/repos/%s/%s/pulls/%d/comments", orgSlug, repoSlug, pr.PublicID))
		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, resp, "**/updated_at", "**/created_at", "**/id", "**/iteration", "**/parent_id")
	})

	t.Run("list pull request comments with sorting", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Kopatych.Identity).
			SetQueryParam("sort_by", "-iteration").
			Get(fmt.Sprintf("/repos/%s/%s/pulls/%d/comments", orgSlug, repoSlug, pr.PublicID))
		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, resp, "**/updated_at", "**/created_at", "**/id", "**/iteration", "**/parent_id")
	})

	t.Run("get pull request comment", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Kopatych.Identity).
			Get(fmt.Sprintf("/pull_comments/id:%s", rootComment.UUID.String()))
		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, resp, "**/updated_at", "**/created_at", "**/id", "**/iteration")
	})

	t.Run("create pull request comment", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Kopatych.Identity).
			SetBody(&pbPub.CreatePullRequestCommentBody{
				Body:      "New comment created via API",
				Iteration: strconv.FormatUint(pr.Iteration, 10),
			}).
			Post(fmt.Sprintf("/repos/%s/%s/pulls/%d/comments", orgSlug, repoSlug, pr.PublicID))
		require.NoError(t, err)
		yarequire.StatusCode(t, resp, err, http.StatusCreated)
		yarequire.HTTPCompareWithFixture(t, resp, "**/updated_at", "**/created_at", "**/id", "**/iteration")
	})

	t.Run("create pull request comment by id", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Kopatych.Identity).
			SetBody(&pbPub.CreatePullRequestCommentBody{
				Body:      "New comment created via ID-based API",
				Iteration: strconv.FormatUint(prForIDOps.Iteration, 10),
			}).
			Post(fmt.Sprintf("/pulls/id:%s/comments", prForIDOps.UUID.String()))
		require.NoError(t, err)
		yarequire.StatusCode(t, resp, err, http.StatusCreated)
		yarequire.HTTPCompareWithFixture(t, resp, "**/updated_at", "**/created_at", "**/id", "**/iteration")
	})

	t.Run("create pull request reply comment", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Admin.Identity).
			SetBody(&pbPub.CreatePullRequestCommentBody{
				ParentId:  rootComment.UUID.String(),
				Body:      "Reply comment created via API",
				Iteration: strconv.FormatUint(pr.Iteration, 10),
			}).
			Post(fmt.Sprintf("/repos/%s/%s/pulls/%d/comments", orgSlug, repoSlug, pr.PublicID))
		require.NoError(t, err)
		yarequire.StatusCode(t, resp, err, http.StatusCreated)
		yarequire.HTTPCompareWithFixture(t, resp, "**/updated_at", "**/created_at", "**/id", "**/iteration", "**/parent_id")
	})

	t.Run("create pull request comment with need resolution", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Admin.Identity).
			SetBody(&pbPub.CreatePullRequestCommentBody{
				Body:           "This needs to be resolved before merge",
				NeedResolution: true,
				Iteration:      strconv.FormatUint(pr.Iteration, 10),
			}).
			Post(fmt.Sprintf("/repos/%s/%s/pulls/%d/comments", orgSlug, repoSlug, pr.PublicID))
		require.NoError(t, err)
		yarequire.StatusCode(t, resp, err, http.StatusCreated)
		yarequire.HTTPCompareWithFixture(t, resp, "**/updated_at", "**/created_at", "**/id", "**/iteration")
	})

	t.Run("create pull request draft comment", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Admin.Identity).
			SetBody(&pbPub.CreatePullRequestCommentBody{
				Body:      "Draft comment not published yet",
				Publish:   false,
				Iteration: strconv.FormatUint(pr.Iteration, 10),
			}).
			Post(fmt.Sprintf("/repos/%s/%s/pulls/%d/comments", orgSlug, repoSlug, pr.PublicID))
		require.NoError(t, err)
		yarequire.StatusCode(t, resp, err, http.StatusCreated)
		yarequire.HTTPCompareWithFixture(t, resp, "**/updated_at", "**/created_at", "**/id", "**/iteration")
	})

	t.Run("create pull request silent comment", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Admin.Identity).
			SetBody(&pbPub.CreatePullRequestCommentBody{
				Body:      "Silent comment without notifications",
				Iteration: strconv.FormatUint(pr.Iteration, 10),
			}).
			SetQueryParam("silent", "true").
			Post(fmt.Sprintf("/repos/%s/%s/pulls/%d/comments", orgSlug, repoSlug, pr.PublicID))
		require.NoError(t, err)
		yarequire.StatusCode(t, resp, err, http.StatusCreated)
		yarequire.HTTPCompareWithFixture(t, resp, "**/updated_at", "**/created_at", "**/id", "**/iteration")
	})

	// Test error cases
	t.Run("get non-existent pull request comment", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Kopatych.Identity).
			Get("/pull_comments/id:00000000-0000-0000-0000-000000000000")
		require.NoError(t, err)
		yarequire.StatusCode(t, resp, err, http.StatusNotFound)
	})

	t.Run("create comment on non-existent pull request", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Kopatych.Identity).
			SetBody(&pbPub.CreatePullRequestCommentBody{
				Body:      "Comment on non-existent PR",
				Iteration: "1",
			}).
			Post(fmt.Sprintf("/repos/%s/%s/pulls/99999/comments", orgSlug, repoSlug))
		require.NoError(t, err)
		yarequire.StatusCode(t, resp, err, http.StatusNotFound)
	})

	t.Run("create comment with invalid parent", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Kopatych.Identity).
			SetBody(&pbPub.CreatePullRequestCommentBody{
				ParentId:  "00000000-0000-0000-0000-000000000000",
				Body:      "Reply to non-existent comment",
				Iteration: strconv.FormatUint(pr.Iteration, 10),
			}).
			Post(fmt.Sprintf("/repos/%s/%s/pulls/%d/comments", orgSlug, repoSlug, pr.PublicID))
		require.NoError(t, err)
		yarequire.StatusCode(t, resp, err, http.StatusNotFound)
	})

	t.Run("unauthorized access", func(t *testing.T) {
		// Test without authentication
		resp, err := suite.gwClient.AsGuest().
			Get(fmt.Sprintf("/repos/%s/%s/pulls/%d/comments", orgSlug, repoSlug, pr.PublicID))
		require.NoError(t, err)
		yarequire.StatusCode(t, resp, err, http.StatusUnauthorized)
	})
}

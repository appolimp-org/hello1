package integrationtests

import (
	"common/testutils/yarequire"
	"fmt"
	"net/http"
	"testing"

	pbPub "private_api/generated/yandex/cloud/public/sourcecraft/v1"

	"github.com/stretchr/testify/require"
)

func (suite *RwApiTestSuite) TestPublicAPIPullRequestGet() {
	t := suite.T()

	// Prepare repo and a pull request
	repo := suite.repos.PrValidation

	// Initialize default branch and create a source branch
	suite.ensureDefaultMaster(repo)
	suite.mustBash(repo, `git checkout -b branch`)

	pr := suite.makePullRequest(suite.users.Krosh, &makePrOptions{
		Repo:   repo,
		Title:  "Public API PR",
		Source: "branch",
		Target: "master",
	})
	pathIgnore := []string{"**/updated_at", "**/created_at", "**/id"}

	t.Run("get", func(t *testing.T) {
		respSlug, err := suite.gwClient.As(suite.users.Kopatych.Identity).
			Get(fmt.Sprintf("/repos/%s/%s/pulls/%d", repo.OrgSlug, repo.Slug, pr.PublicID))
		require.NoError(t, err)

		respID, err := suite.gwClient.As(suite.users.Kopatych.Identity).
			Get(fmt.Sprintf("/pulls/id:%s", pr.UUID.String()))
		require.NoError(t, err)

		yarequire.HTTPCompareWithFixture(t, respSlug, pathIgnore...)
		// check responses are equal
		yarequire.HTTPCompareWithFixture(t, respID, pathIgnore...)
	})
}

func (suite *RwApiTestSuite) TestPublicAPIPullRequestCreate() {
	t := suite.T()

	repo := suite.repos.PrValidation

	// Initialize default branch and create a source branch
	suite.ensureDefaultMaster(repo)
	suite.mustBash(repo, `
		git checkout -b api-test-feature
		echo "API test feature" > api-test.txt
		git add .
		git commit -m "Add API test feature"
	`)

	pathIgnore := []string{"**/updated_at", "**/created_at", "**/id", "**/public_id", "**/slug"}

	t.Run("create with defaults", func(t *testing.T) {
		resp, err := suite.gwClient.As(suite.users.Kopatych.Identity).
			SetBody(pbPub.CreatePullRequestBody{
				Title:        "New feature PR",
				Description:  "This PR adds a new feature",
				SourceBranch: "api-test-feature",
				TargetBranch: "master",
			}).
			Post(fmt.Sprintf("/repos/%s/%s/pulls", repo.OrgSlug, repo.Slug))

		require.NoError(t, err)
		yarequire.StatusCode(t, resp, err, http.StatusCreated)
		yarequire.HTTPCompareWithFixture(t, resp, pathIgnore...)
	})

	t.Run("create with publish", func(t *testing.T) {
		// Create another branch for this test
		suite.mustBash(repo, `
			git checkout -b api-test-publish || git checkout api-test-publish
			echo "Publish test" > publish.txt
			git add .
			git commit -m "Add publish test" || true
		`)

		resp, err := suite.gwClient.As(suite.users.Kopatych.Identity).
			SetBody(pbPub.CreatePullRequestBody{
				Title:        "Published PR",
				Description:  "This PR is published immediately",
				SourceBranch: "api-test-publish",
				TargetBranch: "master",
				Publish:      true,
			}).
			Post(fmt.Sprintf("/repos/id:%s/pulls", repo.UUID))

		require.NoError(t, err)
		yarequire.StatusCode(t, resp, err, http.StatusCreated)
		yarequire.HTTPCompareWithFixture(t, resp, pathIgnore...)
	})

	t.Run("create with reviewer UUID", func(t *testing.T) {
		// Create another branch for this test
		suite.mustBash(repo, `
			git checkout -b api-test-reviewer || git checkout api-test-reviewer
			echo "Reviewer test" > reviewer.txt
			git add .
			git commit -m "Add reviewer test" || true
		`)

		// Use Krosh as a reviewer (UUID conversion test)
		resp, err := suite.gwClient.As(suite.users.Kopatych.Identity).
			SetBody(pbPub.CreatePullRequestBody{
				Title:        "PR with reviewer",
				Description:  "This PR has a reviewer specified by UUID",
				SourceBranch: "api-test-reviewer",
				TargetBranch: "master",
				ReviewerIds:  []string{suite.users.Krosh.UUID.String()},
				Publish:      true,
			}).
			Post(fmt.Sprintf("/repos/%s/%s/pulls", repo.OrgSlug, repo.Slug))

		require.NoError(t, err)
		yarequire.StatusCode(t, resp, err, http.StatusCreated)
		yarequire.HTTPCompareWithFixture(t, resp, pathIgnore...)
	})

	t.Run("error cases", func(t *testing.T) {
		// Test invalid branch
		resp, err := suite.gwClient.As(suite.users.Kopatych.Identity).
			SetBody(pbPub.CreatePullRequestBody{
				Title:        "Invalid branch PR",
				Description:  "This PR has invalid source branch",
				SourceBranch: "nonexistent-branch-12345",
				TargetBranch: "master",
			}).
			Post(fmt.Sprintf("/repos/%s/%s/pulls", repo.OrgSlug, repo.Slug))

		require.NoError(t, err)
		yarequire.StatusCode(t, resp, err, http.StatusNotFound)

		// Test missing title
		resp, err = suite.gwClient.As(suite.users.Kopatych.Identity).
			SetBody(pbPub.CreatePullRequestBody{
				Description:  "This PR has no title",
				SourceBranch: "api-test-feature",
				TargetBranch: "master",
			}).
			Post(fmt.Sprintf("/repos/%s/%s/pulls", repo.OrgSlug, repo.Slug))

		require.NoError(t, err)
		yarequire.StatusCode(t, resp, err, http.StatusBadRequest)

		// Test unauthorized user
		resp, err = suite.gwClient.AsGuest().
			SetBody(pbPub.CreatePullRequestBody{
				Title:        "Unauthorized PR",
				Description:  "This PR should fail due to permissions",
				SourceBranch: "api-test-feature",
				TargetBranch: "master",
			}).
			Post(fmt.Sprintf("/repos/%s/%s/pulls", repo.OrgSlug, repo.Slug))

		require.NoError(t, err)
		yarequire.StatusCode(t, resp, err, http.StatusUnauthorized)
	})
}

func (suite *RwApiTestSuite) TestPublicAPIPullRequestUpdate() {
	t := suite.T()

	// Prepare repo and a pull request
	repo := suite.repos.PrValidation
	publishFalse := false

	// Initialize default branch and create a source branch
	suite.ensureDefaultMaster(repo)
	suite.mustBash(repo, `
		git checkout -b update-test-branch
		echo "Update test content" > update-test.txt
		git add .
		git commit -m "Add update test content"
	`)

	// Create a draft PR for testing updates
	pr := suite.makePullRequest(suite.users.Krosh, &makePrOptions{
		Repo:        repo,
		Title:       "Original Title",
		Description: "Original description",
		Source:      "update-test-branch",
		Target:      "master",
		Publish:     &publishFalse,
	})

	t.Run("update title and description", func(t *testing.T) {
		resp, err := suite.gwClient.As(suite.users.Krosh.Identity).
			SetBody(pbPub.UpdatePullRequestBody{
				Title:       "Updated Title",
				Description: "Updated description with more details",
			}).
			Patch(fmt.Sprintf("/repos/%s/%s/pulls/%d", repo.OrgSlug, repo.Slug, pr.PublicID))

		require.NoError(t, err)
		yarequire.StatusCode(t, resp, err, http.StatusOK)
	})

	t.Run("publish PR (change status to open)", func(t *testing.T) {
		resp, err := suite.gwClient.As(suite.users.Krosh.Identity).
			Post(fmt.Sprintf("/pulls/by-id/%s/publish", pr.UUID.String()))

		require.NoError(t, err)
		yarequire.StatusCode(t, resp, err, http.StatusOK)
	})

	t.Run("draft PR (change status back to draft)", func(t *testing.T) {
		resp, err := suite.gwClient.As(suite.users.Krosh.Identity).
			Post(fmt.Sprintf("/repos/%s/%s/pulls/%d/draft", repo.OrgSlug, repo.Slug, pr.PublicID))

		require.NoError(t, err)
		yarequire.StatusCode(t, resp, err, http.StatusOK)
	})

	t.Run("draft PR by ID", func(t *testing.T) {
		// Publish first so we can draft it again
		suite.gwClient.As(suite.users.Krosh.Identity).
			Post(fmt.Sprintf("/pulls/by-id/%s/publish", pr.UUID.String()))

		resp, err := suite.gwClient.As(suite.users.Krosh.Identity).
			Post(fmt.Sprintf("/pulls/by-id/%s/draft", pr.UUID.String()))

		require.NoError(t, err)
		yarequire.StatusCode(t, resp, err, http.StatusOK)
	})

	t.Run("discard PR (change status to discarded)", func(t *testing.T) {
		// Create a new PR to discard
		suite.mustBash(repo, `
			git checkout -b discard-test-branch || git checkout discard-test-branch
			echo "Discard test content" > discard-test.txt
			git add .
			git commit -m "Add discard test content" || true
		`)

		discardPr := suite.makePullRequest(suite.users.Krosh, &makePrOptions{
			Repo:        repo,
			Title:       "PR to be discarded",
			Description: "This PR will be discarded",
			Source:      "discard-test-branch",
			Target:      "master",
			Publish:     &publishFalse,
		})

		resp, err := suite.gwClient.As(suite.users.Krosh.Identity).
			Post(fmt.Sprintf("/pulls/by-id/%s/discard", discardPr.UUID.String()))

		require.NoError(t, err)
		yarequire.StatusCode(t, resp, err, http.StatusOK)
	})

	t.Run("discard PR by slug", func(t *testing.T) {
		// Create another PR to discard using slug
		suite.mustBash(repo, `
			git checkout -b discard-slug-test || git checkout discard-slug-test
			echo "Discard slug test" > discard-slug.txt
			git add .
			git commit -m "Add discard slug test" || true
		`)

		discardPr2 := suite.makePullRequest(suite.users.Krosh, &makePrOptions{
			Repo:        repo,
			Title:       "PR to discard by slug",
			Description: "This PR will be discarded by slug",
			Source:      "discard-slug-test",
			Target:      "master",
			Publish:     &publishFalse,
		})

		resp, err := suite.gwClient.As(suite.users.Krosh.Identity).
			Post(fmt.Sprintf("/repos/%s/%s/pulls/%d/discard", repo.OrgSlug, repo.Slug, discardPr2.PublicID))

		require.NoError(t, err)
		yarequire.StatusCode(t, resp, err, http.StatusOK)
	})

	t.Run("reopen PR (change status to open)", func(t *testing.T) {
		// Create a new PR to discard and then reopen
		suite.mustBash(repo, `
			git checkout -b reopen-test-branch || git checkout reopen-test-branch
			echo "Reopen test content" > reopen-test.txt
			git add .
			git commit -m "Add reopen test content" || true
		`)

		reopenPr := suite.makePullRequest(suite.users.Krosh, &makePrOptions{
			Repo:        repo,
			Title:       "PR to be reopened",
			Description: "This PR will be discarded and then reopened",
			Source:      "reopen-test-branch",
			Target:      "master",
			Publish:     &publishFalse,
		})

		// First publish it
		suite.gwClient.As(suite.users.Krosh.Identity).
			Post(fmt.Sprintf("/pulls/by-id/%s/publish", reopenPr.UUID.String()))

		// Then discard it
		suite.gwClient.As(suite.users.Krosh.Identity).
			Post(fmt.Sprintf("/pulls/by-id/%s/discard", reopenPr.UUID.String()))

		// Now reopen it
		resp, err := suite.gwClient.As(suite.users.Krosh.Identity).
			Post(fmt.Sprintf("/pulls/by-id/%s/reopen", reopenPr.UUID.String()))

		require.NoError(t, err)
		yarequire.StatusCode(t, resp, err, http.StatusOK)
	})

	t.Run("reopen PR by slug", func(t *testing.T) {
		// Create another PR to test reopen using slug
		suite.mustBash(repo, `
			git checkout -b reopen-slug-test || git checkout reopen-slug-test
			echo "Reopen slug test" > reopen-slug.txt
			git add .
			git commit -m "Add reopen slug test" || true
		`)

		reopenPr2 := suite.makePullRequest(suite.users.Krosh, &makePrOptions{
			Repo:        repo,
			Title:       "PR to reopen by slug",
			Description: "This PR will be reopened by slug",
			Source:      "reopen-slug-test",
			Target:      "master",
			Publish:     &publishFalse,
		})

		// First publish it
		suite.gwClient.As(suite.users.Krosh.Identity).
			Post(fmt.Sprintf("/repos/%s/%s/pulls/%d/publish", repo.OrgSlug, repo.Slug, reopenPr2.PublicID))

		// Then discard it
		suite.gwClient.As(suite.users.Krosh.Identity).
			Post(fmt.Sprintf("/repos/%s/%s/pulls/%d/discard", repo.OrgSlug, repo.Slug, reopenPr2.PublicID))

		// Now reopen it
		resp, err := suite.gwClient.As(suite.users.Krosh.Identity).
			Post(fmt.Sprintf("/repos/%s/%s/pulls/%d/reopen", repo.OrgSlug, repo.Slug, reopenPr2.PublicID))

		require.NoError(t, err)
		yarequire.StatusCode(t, resp, err, http.StatusOK)
	})

	t.Run("update with field mask", func(t *testing.T) {
		// Create another PR for field mask testing
		suite.mustBash(repo, `
			git checkout -b mask-test-branch || git checkout mask-test-branch
			echo "Mask test content" > mask-test.txt
			git add .
			git commit -m "Add mask test content" || true
		`)

		maskPr := suite.makePullRequest(suite.users.Krosh, &makePrOptions{
			Repo:        repo,
			Title:       "Mask Test Title",
			Description: "Mask test description",
			Source:      "mask-test-branch",
			Target:      "master",
			Publish:     &publishFalse,
		})

		// Publish PR (change status to open)
		resp, err := suite.gwClient.As(suite.users.Krosh.Identity).
			Post(fmt.Sprintf("/repos/%s/%s/pulls/%d/publish", repo.OrgSlug, repo.Slug, maskPr.PublicID))

		require.NoError(t, err)
		yarequire.StatusCode(t, resp, err, http.StatusOK)
	})

	t.Run("error cases", func(t *testing.T) {
		// Test unauthorized update
		resp, err := suite.gwClient.AsGuest().
			SetBody(pbPub.UpdatePullRequestBody{
				Title: "Unauthorized update",
			}).
			Patch(fmt.Sprintf("/repos/%s/%s/pulls/%d", repo.OrgSlug, repo.Slug, pr.PublicID))

		require.NoError(t, err)
		yarequire.StatusCode(t, resp, err, http.StatusUnauthorized)

		// Test update non-existent PR
		resp, err = suite.gwClient.As(suite.users.Krosh.Identity).
			SetBody(pbPub.UpdatePullRequestBody{
				Title: "This should fail",
			}).
			Patch(fmt.Sprintf("/repos/%s/%s/pulls/99999", repo.OrgSlug, repo.Slug))

		require.NoError(t, err)
		yarequire.StatusCode(t, resp, err, http.StatusNotFound)
	})
}

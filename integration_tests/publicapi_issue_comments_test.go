package integrationtests

import (
	"common/testutils/yarequire"
	"context"
	"fmt"
	"gitcore/internal/entities"
	"net/http"
	pbPub "private_api/generated/yandex/cloud/public/sourcecraft/v1"
	"testing"

	"github.com/stretchr/testify/require"
)

func (suite *RwApiTestSuite) TestPublicAPIIssueCommentsCrud() {
	t := suite.T()

	repoID, orgSlug, repoSlug := suite.makeRandomRepo(t, suite.users.Admin)

	// test issues

	publicIssue := suite.makeIssue(suite.users.Admin, &makeIssueOptions{
		ID:         1001,
		RepoID:     repoID,
		Title:      "First issue",
		Visibility: entities.IssueVisibilities.Public,
		Status:     entities.IssueStatuses.Declined,
	})

	privateIssue := suite.makeIssue(suite.users.Admin, &makeIssueOptions{
		ID:         1002,
		RepoID:     repoID,
		Title:      "Private issue",
		Visibility: entities.IssueVisibilities.Private,
	})

	myCommentInPrivateIssue := suite.makeIssueComment(suite.users.Kopatych, &makeIssueCommentOptions{
		IssueID: &privateIssue.ID,
		Body:    "xxx",
	})

	publicIssuePrepopulatedComments := suite.makeIssue(suite.users.Admin, &makeIssueOptions{
		ID:         1003,
		RepoID:     repoID,
		Title:      "Second issue",
		Visibility: entities.IssueVisibilities.Public,
		Status:     entities.IssueStatuses.Declined,
	})

	_, _ = publicIssue, privateIssue

	comment := suite.makeIssueComment(suite.users.Krosh, &makeIssueCommentOptions{
		IssueID: &publicIssuePrepopulatedComments.ID,
		Body:    "Some comment that has reactions",
	})
	err := suite.Params.IssueCommentService.AddReaction(context.Background(), comment, entities.Reactions.Goose, suite.users.Admin)
	require.NoError(t, err)

	err = suite.Params.IssueCommentService.AddReaction(context.Background(), comment, entities.Reactions.Like, suite.users.Kopatych)
	require.NoError(t, err)

	suite.makeIssueComment(suite.users.Krosh, &makeIssueCommentOptions{
		IssueID: &publicIssuePrepopulatedComments.ID,
		Body:    "Some comment 2 ",
	})
	suite.makeIssueComment(suite.users.Krosh, &makeIssueCommentOptions{
		IssueID: &publicIssuePrepopulatedComments.ID,
		Body:    "Some comment 3 ",
	})
	branchComment := suite.makeIssueComment(suite.users.Krosh, &makeIssueCommentOptions{
		ID:      999,
		IssueID: &publicIssuePrepopulatedComments.ID,
		Body:    "Some comment 4",
	})

	suite.makeIssueComment(suite.users.Krosh, &makeIssueCommentOptions{
		ParentID: &branchComment.ID,
		IssueID:  &publicIssuePrepopulatedComments.ID,
		Body:     "Branch 1",
	})
	suite.makeIssueComment(suite.users.Krosh, &makeIssueCommentOptions{
		ParentID: &branchComment.ID,
		IssueID:  &publicIssuePrepopulatedComments.ID,
		Body:     "Branch 2",
	})
	suite.makeIssueComment(suite.users.Krosh, &makeIssueCommentOptions{
		ParentID: &branchComment.ID,
		IssueID:  &publicIssuePrepopulatedComments.ID,
		Body:     "Branch 3",
	})

	// test comments

	t.Run("list comments", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Kopatych.Identity).
			Get(fmt.Sprintf("/repos/%s/%s/issues/%d/comments", orgSlug, repoSlug, publicIssuePrepopulatedComments.PublicID))
		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, resp, "**/updated_at", "**/created_at", "**/id")
	})

	t.Run("list comments by id", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Kopatych.Identity).
			Get(fmt.Sprintf("/issues/id:%s/comments", publicIssuePrepopulatedComments.UUID.String()))
		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, resp, "**/updated_at", "**/created_at", "**/id")
	})

	t.Run("get comment", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Kopatych.Identity).
			Get(fmt.Sprintf("/issue_comments/id:%s", branchComment.UUID.String()))
		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, resp, "**/updated_at", "**/created_at", "**/id")
	})

	t.Run("create comment", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Kopatych.Identity).SetBody(&pbPub.CreateIssueCommentBody{Body: "Hello"}).
			Post(fmt.Sprintf("/repos/%s/%s/issues/%d/comments", orgSlug, repoSlug, publicIssue.PublicID))
		require.NoError(t, err)

		yarequire.HTTPCompareWithFixture(t, resp, "**/updated_at", "**/created_at", "**/id")
	})

	t.Run("create comment by id", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Kopatych.Identity).SetBody(&pbPub.CreateIssueCommentBody{Body: "Hello"}).
			Post(fmt.Sprintf("/issues/id:%s/comments", publicIssue.UUID.String()))
		require.NoError(t, err)

		yarequire.HTTPCompareWithFixture(t, resp, "**/updated_at", "**/created_at", "**/id")
	})

	t.Run("update comment", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Kopatych.Identity).SetBody(&pbPub.CreateIssueCommentBody{Body: "OLD"}).
			Post(fmt.Sprintf("/repos/%s/%s/issues/%d/comments", orgSlug, repoSlug, publicIssue.PublicID))
		require.NoError(t, err)

		id, err := yarequire.GetStringFromJSON(resp.Body(), "id")
		require.NoError(t, err)

		resp, err = suite.gwClient.
			As(suite.users.Kopatych.Identity).SetBody(&pbPub.UpdateIssueCommentBody{Body: "NEW"}).
			Patch(fmt.Sprintf("/issue_comments/id:%s", id))
		require.NoError(t, err)

		yarequire.HTTPCompareWithFixture(t, resp, "**/updated_at", "**/created_at", "**/id")
	})

	t.Run("react", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Kopatych.Identity).SetBody(&pbPub.CreateIssueCommentBody{Body: "Super issue"}).
			Post(fmt.Sprintf("/repos/%s/%s/issues/%d/comments", orgSlug, repoSlug, publicIssue.PublicID))
		require.NoError(t, err)

		id, err := yarequire.GetStringFromJSON(resp.Body(), "id")
		require.NoError(t, err)

		_, err = suite.gwClient.
			As(suite.users.Kopatych.Identity).SetBody(&pbPub.ModifyReactionBody{Reaction: pbPub.Reaction_robot}).
			Post(fmt.Sprintf("/issue_comments/id:%s/reactions", id))
		require.NoError(t, err)

		_, err = suite.gwClient.
			As(suite.users.Kopatych.Identity).SetBody(&pbPub.ModifyReactionBody{Reaction: pbPub.Reaction_goose}).
			Post(fmt.Sprintf("/issue_comments/id:%s/reactions", id))
		require.NoError(t, err)

		_, err = suite.gwClient.
			As(suite.users.Kopatych.Identity).SetBody(&pbPub.ModifyReactionBody{Reaction: pbPub.Reaction_dislike}).
			Post(fmt.Sprintf("/issue_comments/id:%s/reactions", id))
		require.NoError(t, err)

		resp, err = suite.gwClient.
			As(suite.users.Kopatych.Identity).SetBody(&pbPub.ModifyReactionBody{Reaction: pbPub.Reaction_dislike}).
			Delete(fmt.Sprintf("/issue_comments/id:%s/reactions", id))
		require.NoError(t, err)

		yarequire.HTTPCompareWithFixture(t, resp, "**/updated_at", "**/created_at", "**/id")
	})

	t.Run("react - no access", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Kopatych.Identity).SetBody(&pbPub.ModifyReactionBody{Reaction: pbPub.Reaction_goose}).
			Post(fmt.Sprintf("/issue_comments/id:%s/reactions", myCommentInPrivateIssue.UUID.String()))

		require.NoError(t, err)
		yarequire.StatusCode(t, resp, err, http.StatusForbidden)
	})

	t.Run("delete comment - happy path", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Kopatych.Identity).SetBody(&pbPub.CreateIssueCommentBody{Body: "OLD"}).
			Post(fmt.Sprintf("/repos/%s/%s/issues/%d/comments", orgSlug, repoSlug, publicIssue.PublicID))
		require.NoError(t, err)

		id, err := yarequire.GetStringFromJSON(resp.Body(), "id")
		require.NoError(t, err)

		resp, err = suite.gwClient.
			As(suite.users.Kopatych.Identity).
			Delete(fmt.Sprintf("/issue_comments/id:%s", id))

		require.NoError(t, err)
		yarequire.StatusCode(t, resp, err, http.StatusNoContent)

		resp, err = suite.gwClient.
			As(suite.users.Kopatych.Identity).
			Get(fmt.Sprintf("/issue_comments/id:%s", id))

		require.NoError(t, err)
		yarequire.StatusCode(t, resp, err, http.StatusOK)
		yarequire.HTTPCompareWithFixture(t, resp, "**/updated_at", "**/created_at", "**/id")
	})

	t.Run("delete comment - no access", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Kopatych.Identity).
			Delete(fmt.Sprintf("/issue_comments/id:%s", comment.UUID.String()))

		require.NoError(t, err)
		yarequire.StatusCode(t, resp, err, http.StatusForbidden)
	})

	t.Run("update comment - no access", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Kopatych.Identity).
			SetBody(&pbPub.UpdateIssueCommentBody{Body: "YYY"}).
			Patch(fmt.Sprintf("/issue_comments/id:%s", comment.UUID.String()))

		require.NoError(t, err)
		yarequire.StatusCode(t, resp, err, http.StatusForbidden)
	})

	t.Run("delete comment - private issue", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Kopatych.Identity).
			Delete(fmt.Sprintf("/issue_comments/id:%s", myCommentInPrivateIssue.UUID.String()))

		require.NoError(t, err)
		yarequire.StatusCode(t, resp, err, http.StatusForbidden)
	})

	t.Run("create branch", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Kopatych.Identity).SetBody(&pbPub.CreateIssueCommentBody{Body: "Parent"}).
			Post(fmt.Sprintf("/repos/%s/%s/issues/%d/comments", orgSlug, repoSlug, publicIssue.PublicID))
		require.NoError(t, err)

		bUUID, err := yarequire.GetUUIDFromJSON(resp.Body(), "id")
		require.NoError(t, err)

		resp, err = suite.gwClient.
			As(suite.users.Kopatych.Identity).SetBody(&pbPub.CreateIssueCommentBody{Body: "Child", ParentId: bUUID.String()}).
			Post(fmt.Sprintf("/repos/%s/%s/issues/%d/comments", orgSlug, repoSlug, publicIssue.PublicID))
		require.NoError(t, err)

		pUUID, err := yarequire.GetUUIDFromJSON(resp.Body(), "parent.id")
		require.NoError(t, err)
		require.Equal(t, pUUID, bUUID)

		yarequire.HTTPCompareWithFixture(t, resp, "**/updated_at", "**/created_at", "**/id", "parent/id")
	})

	t.Run("no access", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Kopatych.Identity).
			Get(fmt.Sprintf("/repos/%s/%s/issues/%d/comments", orgSlug, repoSlug, privateIssue.PublicID))
		yarequire.StatusCode(t, resp, err, http.StatusForbidden)

		resp, err = suite.gwClient.
			As(suite.users.Kopatych.Identity).SetBody(&pbPub.CreateIssueCommentBody{Body: "Hello"}).
			Post(fmt.Sprintf("/repos/%s/%s/issues/%d/comments", orgSlug, repoSlug, privateIssue.PublicID))
		yarequire.StatusCode(t, resp, err, http.StatusForbidden)
	})
}

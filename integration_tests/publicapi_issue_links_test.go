package integrationtests

import (
	"common/grpc"
	"common/testutils/yarequire"
	"context"
	"fmt"
	"gitcore/internal/entities"
	except "gitcore/internal/exceptions"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	pbPub "private_api/generated/yandex/cloud/public/sourcecraft/v1"
	"testing"

	"github.com/stretchr/testify/require"
)

func (suite *RwApiTestSuite) TestPublicAPIIssueLinks() {
	t := suite.T()

	repoID, orgSlug, repoSlug := suite.makeRandomRepo(t, suite.users.Admin)

	// test issues

	parent := suite.makeIssue(suite.users.Admin, &makeIssueOptions{
		ID:     1001,
		RepoID: repoID,
	})
	childIssue1 := suite.makeIssue(suite.users.Admin, &makeIssueOptions{
		ID:     1002,
		RepoID: repoID,
	})
	childIssue2 := suite.makeIssue(suite.users.Admin, &makeIssueOptions{
		ID:     1003,
		RepoID: repoID,
	})

	h1 := suite.makeIssue(suite.users.Kopatych, &makeIssueOptions{
		ID:     1004,
		RepoID: repoID,
	})
	h2 := suite.makeIssue(suite.users.Kopatych, &makeIssueOptions{
		ID:     1005,
		RepoID: repoID,
	})
	h3 := suite.makeIssue(suite.users.Kopatych, &makeIssueOptions{
		ID:     1010,
		RepoID: repoID,
	})
	h4 := suite.makeIssue(suite.users.Kopatych, &makeIssueOptions{
		ID:     1011,
		RepoID: repoID,
	})
	h5 := suite.makeIssue(suite.users.Kopatych, &makeIssueOptions{
		ID:     1012,
		RepoID: repoID,
	})

	forbiddenIssue := suite.makeIssue(suite.users.Krosh, &makeIssueOptions{
		ID:     1007,
		RepoID: repoID,
	})

	forbiddenLinkID, err := suite.Params.IssueLinkRepo.Create(context.Background(), &entities.IssueLink{
		LeftIssueID:  parent.ID,
		RightIssueID: childIssue1.ID,
		LinkType:     entities.IssueLinkTypes.ParentOf,
		AuthorID:     suite.users.Admin.ID, UpdatedBy: suite.users.Admin.ID})

	require.NoError(t, err)

	forbiddenLink, err := suite.Params.IssueLinkRepo.Get(context.Background(), forbiddenLinkID)
	require.NoError(t, err)

	_, err = suite.Params.IssueLinkRepo.Create(context.Background(), &entities.IssueLink{
		LeftIssueID:  parent.ID,
		RightIssueID: childIssue2.ID,
		LinkType:     entities.IssueLinkTypes.ParentOf,
		AuthorID:     suite.users.Admin.ID, UpdatedBy: suite.users.Admin.ID})

	require.NoError(t, err)

	deletableLinkID, err := suite.Params.IssueLinkRepo.Create(context.Background(), &entities.IssueLink{
		LeftIssueID:  h1.ID,
		RightIssueID: h5.ID,
		LinkType:     entities.IssueLinkTypes.Relates,
		AuthorID:     suite.users.Kopatych.ID, UpdatedBy: suite.users.Kopatych.ID})
	require.NoError(t, err)

	deletableLink, err := suite.Params.IssueLinkRepo.Get(context.Background(), deletableLinkID)
	require.NoError(t, err)

	deletedIssue := suite.makeIssue(suite.users.Kopatych, &makeIssueOptions{
		RepoID:     repoID,
		Title:      "Deleted issue",
		Visibility: entities.IssueVisibilities.Public,
	})
	err = suite.IssueService.Delete(context.Background(), deletedIssue, suite.users.Admin.ID, entities.NotifyOptions{})
	require.NoError(t, err)

	t.Run("list links", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Kopatych.Identity).
			Get(fmt.Sprintf("/repos/%s/%s/issues/%d/issue_links", orgSlug, repoSlug, parent.PublicID))
		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, resp, "**/updated_at", "**/created_at", "**/id")
	})

	t.Run("list links (inverse)", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Kopatych.Identity).
			Get(fmt.Sprintf("/repos/%s/%s/issues/%d/issue_links", orgSlug, repoSlug, childIssue1.PublicID))
		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, resp, "**/updated_at", "**/created_at", "**/id")
	})

	t.Run("list links by id", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Kopatych.Identity).
			Get(fmt.Sprintf("/issues/id:%s/issue_links", parent.UUID.String()))
		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, resp, "**/updated_at", "**/created_at", "**/id")
	})

	t.Run("create - slug", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Kopatych.Identity).
			SetBody(&pbPub.CreateLinkBody{
				TargetIssueSlug: grpc.MarshalID(h2.PublicID),
				LinkType:        pbPub.IssueLink_related_to,
			}).
			Post(fmt.Sprintf("/repos/%s/%s/issues/%d/issue_links", orgSlug, repoSlug, h1.PublicID))
		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, resp, "**/updated_at", "**/created_at", "**/id")
	})

	t.Run("create - id", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Kopatych.Identity).
			SetBody(&pbPub.CreateLinkBody{
				TargetIssueId: grpc_marshalling.UUIDInverse(h3.UUID),
				LinkType:      pbPub.IssueLink_related_to,
			}).
			Post(fmt.Sprintf("/issues/id:%s/issue_links", h1.UUID.String()))
		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, resp, "**/updated_at", "**/created_at", "**/id")
	})

	t.Run("create - duplicate", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Kopatych.Identity).
			SetBody(&pbPub.CreateLinkBody{
				TargetIssueId: grpc_marshalling.UUIDInverse(h4.UUID),
				LinkType:      pbPub.IssueLink_related_to,
			}).
			Post(fmt.Sprintf("/repos/%s/%s/issues/%d/issue_links", orgSlug, repoSlug, h1.PublicID))
		yarequire.StatusCode(t, resp, err, 201)

		resp, err = suite.gwClient.
			As(suite.users.Kopatych.Identity).
			SetBody(&pbPub.CreateLinkBody{
				TargetIssueId: grpc_marshalling.UUIDInverse(h4.UUID),
				LinkType:      pbPub.IssueLink_related_to,
			}).
			Post(fmt.Sprintf("/repos/%s/%s/issues/%d/issue_links", orgSlug, repoSlug, h1.PublicID))

		yarequire.StatusCode(t, resp, err, 409) // conflict
	})

	t.Run("create: forbidden", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Kopatych.Identity).
			SetBody(&pbPub.CreateLinkBody{
				TargetIssueId: grpc_marshalling.UUIDInverse(forbiddenIssue.UUID),
				LinkType:      pbPub.IssueLink_related_to,
			}).
			Post(fmt.Sprintf("/repos/%s/%s/issues/%d/issue_links", orgSlug, repoSlug, h1.PublicID))
		yarequire.StatusCode(t, resp, err, 403)

	})

	t.Run("create: target is deleted", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Kopatych.Identity).
			SetBody(&pbPub.CreateLinkBody{
				TargetIssueId: grpc_marshalling.UUIDInverse(deletedIssue.UUID),
				LinkType:      pbPub.IssueLink_related_to,
			}).
			Post(fmt.Sprintf("/repos/%s/%s/issues/%d/issue_links", orgSlug, repoSlug, h1.PublicID))
		yarequire.StatusCode(t, resp, err, 404)
	})

	t.Run("delete", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Kopatych.Identity).
			Delete(fmt.Sprintf("/issue_links/id:%s", deletableLink.UUID.String()))

		yarequire.StatusCode(t, resp, err, 204)

		_, err = suite.Params.IssueLinkRepo.Get(context.Background(), deletableLink.ID)
		require.ErrorIs(t, err, except.EntityNotFound)

	})

	t.Run("delete: forbidden", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Kopatych.Identity).
			Delete(fmt.Sprintf("/issue_links/id:%s", forbiddenLink.UUID))

		yarequire.StatusCode(t, resp, err, 403)
	})

}

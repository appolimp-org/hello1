package integrationtests

import (
	"common/testutils/yarequire"
	"fmt"
	"gitcore/internal/entities"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"net/http"
	pbPub "private_api/generated/yandex/cloud/public/sourcecraft/v1"
	"testing"

	"github.com/stretchr/testify/require"
)

func (suite *RwApiTestSuite) TestPublicAPIIssueLinkedPRs() {
	t := suite.T()
	user1 := suite.users.Krosh

	repoID, orgSlug, repoSlug := suite.makeRandomRepo(t, user1)
	repoID2, _, _ := suite.makeRandomRepo(t, user1)

	pr1 := suite.makeFauxPullRequest(repoID, user1, &makeFauxPrOptions{Title: "PR-1", PublicID: 1, Source: "branch-1", Target: "master"})
	pr2 := suite.makeFauxPullRequest(repoID, user1, &makeFauxPrOptions{Title: "PR-2", PublicID: 2, Source: "branch-2", Target: "master"})
	pr3 := suite.makeFauxPullRequest(repoID, user1, &makeFauxPrOptions{Title: "PR-3", PublicID: 3, Source: "branch-3", Target: "master"})
	idorPR := suite.makeFauxPullRequest(repoID2, user1, &makeFauxPrOptions{Title: "PR-1", PublicID: 10, Source: "branch-3", Target: "master"})

	issue1 := suite.makeIssue(suite.users.Admin, &makeIssueOptions{
		ID:             2000,
		RepoID:         repoID,
		Title:          "Issue0",
		Visibility:     entities.IssueVisibilities.Public,
		PullRequestIDs: []uint64{},
	})

	issue2 := suite.makeIssue(suite.users.Admin, &makeIssueOptions{
		ID:             2001,
		RepoID:         repoID,
		Title:          "Issue1",
		Visibility:     entities.IssueVisibilities.Public,
		PullRequestIDs: []uint64{pr1.ID, pr3.ID},
	})

	issue3 := suite.makeIssue(suite.users.Admin, &makeIssueOptions{
		ID:             2002,
		RepoID:         repoID,
		Title:          "Issue1",
		Visibility:     entities.IssueVisibilities.Public,
		PullRequestIDs: []uint64{pr1.ID, pr3.ID},
	})

	issue4 := suite.makeIssue(suite.users.Admin, &makeIssueOptions{
		ID:             2003,
		RepoID:         repoID,
		Title:          "Issue1",
		Visibility:     entities.IssueVisibilities.Public,
		PullRequestIDs: []uint64{pr1.ID, pr3.ID},
	})

	t.Run("get", func(t *testing.T) {
		r, err := suite.gwClient.As(suite.users.Admin.Identity).
			Get(fmt.Sprintf("/repos/%s/%s/issues/%d/linked_prs", orgSlug, repoSlug, issue2.PublicID))
		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, r, "**/created_at", "**/id", "**/updated_at", "**/slug")
	})

	t.Run("get by id", func(t *testing.T) {
		r, err := suite.gwClient.As(suite.users.Admin.Identity).
			Get(fmt.Sprintf("/issues/id:%s/linked_prs", issue2.UUID.String()))
		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, r, "**/created_at", "**/id", "**/updated_at", "**/slug")
	})

	t.Run("add", func(t *testing.T) {
		r, err := suite.gwClient.As(suite.users.Admin.Identity).
			SetBody(pbPub.ModifyPullRequestCollectionRequest{Slugs: []string{
				grpc_marshalling.IDInverse(pr1.PublicID),
				grpc_marshalling.IDInverse(pr2.PublicID)}}).
			Post(fmt.Sprintf("/repos/%s/%s/issues/%d/linked_prs", orgSlug, repoSlug, issue1.PublicID))
		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, r, "**/created_at", "**/id", "**/updated_at", "**/slug")
	})

	t.Run("add by id", func(t *testing.T) {
		r, err := suite.gwClient.As(suite.users.Admin.Identity).
			SetBody(pbPub.ModifyPullRequestCollectionRequest{Ids: []string{
				grpc_marshalling.UUIDInverse(pr1.UUID),
				grpc_marshalling.UUIDInverse(pr2.UUID)}}).
			Post(fmt.Sprintf("/issues/id:%s/linked_prs", issue1.UUID.String()))
		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, r, "**/created_at", "**/id", "**/updated_at", "**/slug")
	})

	t.Run("add - IDOR", func(t *testing.T) {
		r, err := suite.gwClient.As(suite.users.Admin.Identity).
			SetBody(pbPub.ModifyPullRequestCollectionRequest{Slugs: []string{
				grpc_marshalling.IDInverse(pr1.PublicID),
				grpc_marshalling.IDInverse(pr2.PublicID),
				grpc_marshalling.IDInverse(idorPR.PublicID),
			}}).
			Post(fmt.Sprintf("/repos/%s/%s/issues/%d/linked_prs", orgSlug, repoSlug, issue1.PublicID))
		require.NoError(t, err)

		yarequire.StatusCode(t, r, err, http.StatusNotFound)
	})

	// not implemented - yet
	_ = issue3

	//t.Run("replace", func(t *testing.T) {
	//	r, err := suite.gwClient.As(suite.users.Admin.Identity).
	//		SetBody(pbPub.ModifyPullRequestCollectionRequest{Slugs: []string{
	//			grpc_marshalling.IDInverse(pr1.PublicID),
	//			grpc_marshalling.IDInverse(pr2.PublicID)}}).
	//		Post(fmt.Sprintf("/repos/%s/%s/issues/%d/prs", orgSlug, repoSlug, issue3.PublicID))
	//	require.NoError(t, err)
	//	yarequire.HTTPCompareWithFixture(t, r)

	t.Run("delete", func(t *testing.T) {
		r, err := suite.gwClient.As(suite.users.Admin.Identity).
			SetBody(pbPub.ModifyPullRequestCollectionRequest{Slugs: []string{
				grpc_marshalling.IDInverse(pr1.PublicID),
				grpc_marshalling.IDInverse(pr2.PublicID)}}).
			Delete(fmt.Sprintf("/repos/%s/%s/issues/%d/linked_prs", orgSlug, repoSlug, issue4.PublicID))

		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, r, "**/created_at", "**/id", "**/updated_at", "**/slug")
	})

	t.Run("delete by id", func(t *testing.T) {
		r, err := suite.gwClient.As(suite.users.Admin.Identity).
			SetBody(pbPub.ModifyPullRequestCollectionRequest{Ids: []string{
				grpc_marshalling.UUIDInverse(pr1.UUID),
				grpc_marshalling.UUIDInverse(pr2.UUID)}}).
			Delete(fmt.Sprintf("/issues/id:%s/linked_prs", issue3.UUID.String()))

		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, r, "**/created_at", "**/id", "**/updated_at", "**/slug")
	})
}

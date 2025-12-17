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

func (suite *RwApiTestSuite) TestPublicAPIIssueLabelsCollection() {
	t := suite.T()

	repoID, orgSlug, repoSlug := suite.makeRandomRepo(t, suite.users.Admin)
	repoID2, _, _ := suite.makeRandomRepo(t, suite.users.Admin)

	taskLabel := suite.makeLabel(suite.users.Admin, &makeLabelOptions{
		ID:     1000,
		RepoID: repoID,
		Name:   "Task",
		Color:  entities.PresetLabelColors.Grey,
	})
	bugLabel := suite.makeLabel(suite.users.Admin, &makeLabelOptions{
		ID:     1001,
		RepoID: repoID,
		Name:   "Bug",
		Color:  entities.PresetLabelColors.Red,
	})
	featureLabel := suite.makeLabel(suite.users.Admin, &makeLabelOptions{
		ID:     1002,
		RepoID: repoID,
		Name:   "Feature",
		Color:  entities.PresetLabelColors.Green,
	})

	idorLabel := suite.makeLabel(suite.users.Admin, &makeLabelOptions{
		ID:     1003,
		RepoID: repoID2,
		Name:   "XXX",
		Color:  entities.PresetLabelColors.Green,
	})

	issue1 := suite.makeIssue(suite.users.Admin, &makeIssueOptions{
		ID:         2000,
		RepoID:     repoID,
		Title:      "Issue0",
		Visibility: entities.IssueVisibilities.Public,
		LabelIDs:   []uint64{},
	})

	issue2 := suite.makeIssue(suite.users.Admin, &makeIssueOptions{
		ID:         2001,
		RepoID:     repoID,
		Title:      "Issue1",
		Visibility: entities.IssueVisibilities.Public,
		LabelIDs:   []uint64{taskLabel.ID, bugLabel.ID}})

	issue3 := suite.makeIssue(suite.users.Admin, &makeIssueOptions{
		ID:         2002,
		RepoID:     repoID,
		Title:      "Issue1",
		Visibility: entities.IssueVisibilities.Public,
		LabelIDs:   []uint64{taskLabel.ID, featureLabel.ID, bugLabel.ID}})

	t.Run("get", func(t *testing.T) {
		r, err := suite.gwClient.As(suite.users.Admin.Identity).
			Get(fmt.Sprintf("/repos/%s/%s/issues/%d/labels", orgSlug, repoSlug, issue2.PublicID))
		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, r, "**/created_at", "**/updated_at", "**/id")
	})

	t.Run("get by id", func(t *testing.T) {
		r, err := suite.gwClient.As(suite.users.Admin.Identity).
			Get(fmt.Sprintf("/issues/id:%s/labels", issue2.UUID.String()))
		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, r, "**/created_at", "**/updated_at", "**/id")
	})

	t.Run("add", func(t *testing.T) {
		r, err := suite.gwClient.As(suite.users.Admin.Identity).
			SetBody(pbPub.ModifyLabelCollectionRequest{Slugs: []string{
				*featureLabel.Slug,
				*bugLabel.Slug}}).
			Post(fmt.Sprintf("/repos/%s/%s/issues/%d/labels", orgSlug, repoSlug, issue1.PublicID))
		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, r, "**/created_at", "**/updated_at", "**/id")
	})

	t.Run("add by id", func(t *testing.T) {
		r, err := suite.gwClient.As(suite.users.Admin.Identity).
			SetBody(pbPub.ModifyLabelCollectionRequest{Ids: []string{
				grpc_marshalling.UUIDInverse(featureLabel.UUID),
				grpc_marshalling.UUIDInverse(bugLabel.UUID)}}).
			Post(fmt.Sprintf("/issues/id:%s/labels", issue1.UUID.String()))
		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, r, "**/created_at", "**/updated_at", "**/id")
	})

	t.Run("add - IDOR", func(t *testing.T) {
		r, err := suite.gwClient.As(suite.users.Admin.Identity).
			SetBody(pbPub.ModifyLabelCollectionRequest{Ids: []string{
				grpc_marshalling.UUIDInverse(featureLabel.UUID),
				grpc_marshalling.UUIDInverse(idorLabel.UUID)}}).
			Post(fmt.Sprintf("/repos/%s/%s/issues/%d/labels", orgSlug, repoSlug, issue1.PublicID))
		require.NoError(t, err)

		yarequire.StatusCode(t, r, err, http.StatusNotFound)
	})

	t.Run("replace", func(t *testing.T) {
		r, err := suite.gwClient.As(suite.users.Admin.Identity).
			SetBody(pbPub.ModifyLabelCollectionRequest{Slugs: []string{
				*featureLabel.Slug, *bugLabel.Slug}}).
			Put(fmt.Sprintf("/repos/%s/%s/issues/%d/labels", orgSlug, repoSlug, issue2.PublicID))
		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, r, "**/created_at", "**/updated_at", "**/id")
	})

	t.Run("replace by id", func(t *testing.T) {
		r, err := suite.gwClient.As(suite.users.Admin.Identity).
			SetBody(pbPub.ModifyLabelCollectionRequest{Ids: []string{
				grpc_marshalling.UUIDInverse(featureLabel.UUID), grpc_marshalling.UUIDInverse(bugLabel.UUID)}}).
			Put(fmt.Sprintf("/issues/id:%s/labels", issue2.UUID.String()))
		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, r, "**/created_at", "**/updated_at", "**/id")
	})

	t.Run("replace - IDOR", func(t *testing.T) {
		r, err := suite.gwClient.As(suite.users.Admin.Identity).
			SetBody(pbPub.ModifyLabelCollectionRequest{Ids: []string{
				grpc_marshalling.UUIDInverse(featureLabel.UUID), grpc_marshalling.UUIDInverse(idorLabel.UUID)}}).
			Put(fmt.Sprintf("/repos/%s/%s/issues/%d/labels", orgSlug, repoSlug, issue2.PublicID))
		require.NoError(t, err)
		yarequire.StatusCode(t, r, err, http.StatusNotFound)
	})

	t.Run("delete", func(t *testing.T) {
		r, err := suite.gwClient.As(suite.users.Admin.Identity).
			SetBody(pbPub.ModifyLabelCollectionRequest{Slugs: []string{
				*featureLabel.Slug, *bugLabel.Slug}}).
			Delete(fmt.Sprintf("/repos/%s/%s/issues/%d/labels", orgSlug, repoSlug, issue3.PublicID))
		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, r, "**/created_at", "**/updated_at", "**/id")
	})

	t.Run("delete by id", func(t *testing.T) {
		r, err := suite.gwClient.As(suite.users.Admin.Identity).
			SetBody(pbPub.ModifyLabelCollectionRequest{Ids: []string{
				grpc_marshalling.UUIDInverse(featureLabel.UUID), grpc_marshalling.UUIDInverse(bugLabel.UUID)}}).
			Delete(fmt.Sprintf("/issues/id:%s/labels", issue3.UUID.String()))
		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, r, "**/created_at", "**/updated_at", "**/id")
	})
}

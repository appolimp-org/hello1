package integrationtests

import (
	"common/testutils/yarequire"
	"context"
	"fmt"
	"gitcore/internal/entities"
	pbPub "private_api/generated/yandex/cloud/public/sourcecraft/v1"
	"testing"

	"github.com/stretchr/testify/require"
)

func (suite *RwApiTestSuite) TestPublicAPILabelService() {
	t := suite.T()

	repoID, orgSlug, repoSlug := suite.makeRandomRepo(t, suite.users.Admin)
	repo, err := suite.RepoService.Get(context.Background(), repoID)
	require.NoError(t, err)
	anotherRepoID, _, _ := suite.makeRandomRepo(t, suite.users.Admin)

	labels := []*entities.Label{
		suite.makeLabel(suite.users.Admin, &makeLabelOptions{
			RepoID: repoID,
			Name:   "Bug",
			Color:  entities.PresetLabelColors.Red,
		}),
		suite.makeLabel(suite.users.Kopatych, &makeLabelOptions{
			RepoID: repoID,
			Name:   "Feature",
			Color:  entities.PresetLabelColors.Green,
		}),
		suite.makeLabel(suite.users.Krosh, &makeLabelOptions{
			RepoID: repoID,
			Name:   "Task",
			Color:  entities.PresetLabelColors.Grey,
		}),
	}

	deletedLabel := suite.makeLabel(suite.users.Krosh, &makeLabelOptions{
		RepoID: repoID,
		Name:   "Deleted",
		Color:  entities.PresetLabelColors.Grey,
	})
	err = suite.LabelRepo.Delete(context.Background(), deletedLabel.ID)
	require.NoError(t, err)

	suite.makeLabel(suite.users.Kopatych, &makeLabelOptions{
		RepoID: anotherRepoID,
		Name:   "Another repo label",
		Color:  entities.PresetLabelColors.Red,
	})

	t.Run("list labels: repo slug", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Admin.Identity).
			Get(fmt.Sprintf("/repos/%s/%s/labels", orgSlug, repoSlug))
		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, resp, "**/updated_at", "**/created_at", "**/id")
	})

	t.Run("list labels: repo id", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Kopatych.Identity).
			Get(fmt.Sprintf("/repos/id:%s/labels", repo.UUID.String()))
		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, resp, "**/updated_at", "**/created_at", "**/id")
	})

	t.Run("create label: defaults", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Admin.Identity).
			SetBody(pbPub.CreateLabelBody{
				Name: "New Label",
			}).
			Post(fmt.Sprintf("/repos/%s/%s/labels", orgSlug, repoSlug))
		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, resp, "**/updated_at", "**/created_at", "**/id")
	})

	t.Run("create label: explicit", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Admin.Identity).
			SetBody(pbPub.CreateLabelBody{
				Name:  "Explicit Label",
				Slug:  "explicit-label",
				Color: "FF00FF",
			}).
			Post(fmt.Sprintf("/repos/%s/%s/labels", orgSlug, repoSlug))
		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, resp, "**/updated_at", "**/created_at", "**/id")
	})

	t.Run("create duplicate label name", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Admin.Identity).
			SetBody(pbPub.CreateLabelBody{
				Name: labels[1].Name,
			}).
			Post(fmt.Sprintf("/repos/%s/%s/labels", orgSlug, repoSlug))
		yarequire.StatusCode(t, resp, err, 409)
	})

	t.Run("get label: by slug", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Kopatych.Identity).
			Get(fmt.Sprintf("/repos/%s/%s/labels/%s", orgSlug, repoSlug, *labels[0].Slug))
		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, resp, "**/updated_at", "**/created_at", "**/id")
	})

	t.Run("get label: by id", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Kopatych.Identity).
			Get(fmt.Sprintf("/labels/id:%s", labels[1].UUID.String()))
		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, resp, "**/updated_at", "**/created_at", "**/id")
	})

	t.Run("get non-existent label", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Kopatych.Identity).
			Get(fmt.Sprintf("/repos/%s/%s/labels/non-existent", orgSlug, repoSlug))
		yarequire.StatusCode(t, resp, err, 404)
	})

	t.Run("update label: by slug", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Admin.Identity).
			SetBody(pbPub.UpdateLabelBody{
				Name:  "Updated Name",
				Color: "00FF00",
			}).
			Patch(fmt.Sprintf("/repos/%s/%s/labels/%s", orgSlug, repoSlug, *labels[2].Slug))
		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, resp, "**/updated_at", "**/created_at", "**/id")
	})

	t.Run("update label: by id", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Admin.Identity).
			SetBody(pbPub.UpdateLabelBody{
				Name:  "Updated Name 2",
				Color: "0000FF",
			}).
			Patch(fmt.Sprintf("/labels/id:%s", labels[0].UUID.String()))
		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, resp, "**/updated_at", "**/created_at", "**/id")
	})

	t.Run("delete label: by slug", func(t *testing.T) {
		newLabel := suite.makeLabel(suite.users.Kopatych, &makeLabelOptions{
			RepoID: repoID,
			Name:   "ToDelete",
			Color:  entities.PresetLabelColors.Grey,
		})

		resp, err := suite.gwClient.
			As(suite.users.Kopatych.Identity).
			Delete(fmt.Sprintf("/repos/%s/%s/labels/%s", orgSlug, repoSlug, *newLabel.Slug))
		yarequire.StatusCode(t, resp, err, 204)

		resp, err = suite.gwClient.
			As(suite.users.Kopatych.Identity).
			Get(fmt.Sprintf("/repos/%s/%s/labels/%s", orgSlug, repoSlug, *newLabel.Slug))
		yarequire.StatusCode(t, resp, err, 404)
	})

	t.Run("delete label: by id", func(t *testing.T) {
		newLabel := suite.makeLabel(suite.users.Kopatych, &makeLabelOptions{
			RepoID: repoID,
			Name:   "ToDelete2",
			Color:  entities.PresetLabelColors.Grey,
		})

		resp, err := suite.gwClient.
			As(suite.users.Kopatych.Identity).
			Delete(fmt.Sprintf("/labels/id:%s", newLabel.UUID.String()))
		yarequire.StatusCode(t, resp, err, 204)

		resp, err = suite.gwClient.
			As(suite.users.Kopatych.Identity).
			Get(fmt.Sprintf("/labels/id:%s", newLabel.UUID.String()))
		yarequire.StatusCode(t, resp, err, 404)
	})
}

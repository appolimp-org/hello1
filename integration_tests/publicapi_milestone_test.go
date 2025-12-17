package integrationtests

import (
	"common/testutils/yarequire"
	"context"
	"fmt"
	"gitcore/internal/entities"
	pbPub "private_api/generated/yandex/cloud/public/sourcecraft/v1"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (suite *RwApiTestSuite) TestPublicAPIMilestoneService() {
	t := suite.T()
	repoID, orgSlug, repoSlug := suite.makeRandomRepo(t, suite.users.Admin)
	repo, err := suite.RepoService.Get(context.Background(), repoID)
	require.NoError(t, err)
	anotherRepoID, _, _ := suite.makeRandomRepo(t, suite.users.Admin)

	milestones := []*entities.Milestone{
		suite.makeMilestone(suite.users.Admin, &makeMilestoneOptions{
			RepoID: repoID,
			Name:   "Milestone 1",
		}),
		suite.makeMilestone(suite.users.Kopatych, &makeMilestoneOptions{
			RepoID: repoID,
			Name:   "Milestone 2",
		}),
	}

	deletedMilestone := suite.makeMilestone(suite.users.Krosh, &makeMilestoneOptions{
		RepoID: repoID,
		Name:   "Deleted Milestone",
	})
	err = suite.MilestoneRepo.Delete(context.Background(), deletedMilestone.ID)
	require.NoError(t, err)

	suite.makeMilestone(suite.users.Kopatych, &makeMilestoneOptions{
		RepoID: anotherRepoID,
		Name:   "Another Repo Milestone",
	})

	t.Run("list milestones: repo slug", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Admin.Identity).
			Get(fmt.Sprintf("/repos/%s/%s/milestones", orgSlug, repoSlug))
		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, resp, "**/updated_at", "**/created_at", "**/id")
	})

	t.Run("list milestones: repo id", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Kopatych.Identity).
			Get(fmt.Sprintf("/repos/id:%s/milestones", repo.UUID.String()))
		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, resp, "**/updated_at", "**/created_at", "**/id")
	})

	t.Run("create milestone: defaults", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Admin.Identity).
			SetBody(pbPub.CreateMilestoneBody{
				Name: "New Milestone",
			}).
			Post(fmt.Sprintf("/repos/%s/%s/milestones", orgSlug, repoSlug))
		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, resp, "**/updated_at", "**/created_at", "**/id")
	})

	t.Run("create milestone: with dates", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Admin.Identity).
			SetBody(pbPub.CreateMilestoneBody{
				Name:        "Milestone with Dates",
				Description: "Has start and end dates",
				StartDate:   timestamppb.New(time.Date(2050, 1, 1, 0, 0, 0, 0, time.UTC)),
				Deadline:    timestamppb.New(time.Date(2050, 10, 1, 0, 0, 0, 0, time.UTC)),
			}).
			Post(fmt.Sprintf("/repos/%s/%s/milestones", orgSlug, repoSlug))
		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, resp, "**/updated_at", "**/created_at", "**/id")
	})

	t.Run("create duplicate milestone slug", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Admin.Identity).
			SetBody(pbPub.CreateMilestoneBody{
				Name: "Milestone",
				Slug: *milestones[0].Slug,
			}).
			Post(fmt.Sprintf("/repos/%s/%s/milestones", orgSlug, repoSlug))
		yarequire.StatusCode(t, resp, err, 400)
	})

	t.Run("get milestone: by slug", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Kopatych.Identity).
			Get(fmt.Sprintf("/repos/%s/%s/milestones/%s", orgSlug, repoSlug, *milestones[0].Slug))
		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, resp, "**/updated_at", "**/created_at", "**/id")
	})

	t.Run("get milestone: by id", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Kopatych.Identity).
			Get(fmt.Sprintf("/milestones/id:%s", milestones[1].UUID.String()))
		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, resp, "**/updated_at", "**/created_at", "**/id")
	})

	t.Run("get non-existent milestone", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Kopatych.Identity).
			Get(fmt.Sprintf("/repos/%s/%s/milestones/non-existent", orgSlug, repoSlug))
		yarequire.StatusCode(t, resp, err, 404)
	})

	t.Run("update milestone: by slug", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Admin.Identity).
			SetBody(pbPub.UpdateMilestoneBody{
				Name:        "Updated Name",
				Description: "Updated description",
			}).
			Patch(fmt.Sprintf("/repos/%s/%s/milestones/%s", orgSlug, repoSlug, *milestones[0].Slug))
		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, resp, "**/updated_at", "**/created_at", "**/id")
	})

	t.Run("update milestone: by id", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Admin.Identity).
			SetBody(pbPub.UpdateMilestoneBody{
				Status: pbPub.Milestone_closed,
			}).
			Patch(fmt.Sprintf("/milestones/id:%s", milestones[1].UUID.String()))
		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, resp, "**/updated_at", "**/created_at", "**/id")
	})

	t.Run("delete milestone: by slug", func(t *testing.T) {
		newMilestone := suite.makeMilestone(suite.users.Kopatych, &makeMilestoneOptions{
			RepoID: repoID,
			Name:   "ToDelete",
		})
		resp, err := suite.gwClient.
			As(suite.users.Kopatych.Identity).
			Delete(fmt.Sprintf("/repos/%s/%s/milestones/%s", orgSlug, repoSlug, *newMilestone.Slug))
		yarequire.StatusCode(t, resp, err, 204)
		resp, err = suite.gwClient.
			As(suite.users.Kopatych.Identity).
			Get(fmt.Sprintf("/repos/%s/%s/milestones/%s", orgSlug, repoSlug, *newMilestone.Slug))
		yarequire.StatusCode(t, resp, err, 404)
	})

	t.Run("delete milestone: by id", func(t *testing.T) {
		newMilestone := suite.makeMilestone(suite.users.Kopatych, &makeMilestoneOptions{
			RepoID: repoID,
			Name:   "ToDelete2",
		})
		resp, err := suite.gwClient.
			As(suite.users.Kopatych.Identity).
			Delete(fmt.Sprintf("/milestones/id:%s", newMilestone.UUID.String()))
		yarequire.StatusCode(t, resp, err, 204)
		resp, err = suite.gwClient.
			As(suite.users.Kopatych.Identity).
			Get(fmt.Sprintf("/milestones/id:%s", newMilestone.UUID.String()))
		yarequire.StatusCode(t, resp, err, 404)
	})

	t.Run("list milestones", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Admin.Identity).
			Get(fmt.Sprintf("/repos/%s/%s/milestones", orgSlug, repoSlug))
		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, resp, "**/updated_at", "**/created_at", "**/id")
	})

	t.Run("list milestones with filter: open", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Admin.Identity).
			SetQueryParam("filter", "status=open").
			Get(fmt.Sprintf("/repos/%s/%s/milestones", orgSlug, repoSlug))
		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, resp, "**/updated_at", "**/created_at", "**/id")
	})

	t.Run("list milestones with filter: closed", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Admin.Identity).
			SetQueryParam("filter", "status=closed").
			Get(fmt.Sprintf("/repos/%s/%s/milestones", orgSlug, repoSlug))
		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, resp, "**/updated_at", "**/created_at", "**/id")
	})
}

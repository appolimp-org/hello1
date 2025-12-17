package integrationtests

import (
	"common/grpc"
	"common/testutils/yarequire"
	"context"
	"fmt"
	"gitcore/internal/entities"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	pbPub "private_api/generated/yandex/cloud/public/sourcecraft/v1"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func (suite *RwApiTestSuite) TestPublicAPIListIssues() {
	t := suite.T()

	repoID, orgSlug, repoSlug := suite.makeRandomRepo(t, suite.users.Admin)
	repo, err := suite.RepoService.Get(context.Background(), repoID)
	require.NoError(t, err)

	milestone := suite.makeMilestone(suite.users.Admin, &makeMilestoneOptions{
		RepoID: repoID,
		Name:   "Milestone",
	})

	labelTask := suite.makeLabel(suite.users.Admin, &makeLabelOptions{
		RepoID: repoID,
		Name:   "Task",
		Color:  entities.PresetLabelColors.Grey,
	})
	labelBug := suite.makeLabel(suite.users.Admin, &makeLabelOptions{
		RepoID: repoID,
		Name:   "Bug",
		Color:  entities.PresetLabelColors.Red,
	})
	labelFeature := suite.makeLabel(suite.users.Admin, &makeLabelOptions{
		RepoID: repoID,
		Name:   "Feature",
		Color:  entities.PresetLabelColors.Green,
	})

	// test issues
	issues := []*entities.Issue{
		suite.makeIssue(suite.users.Admin, &makeIssueOptions{
			RepoID:     repoID,
			Title:      "First issue",
			Visibility: entities.IssueVisibilities.Public,
			Status:     entities.IssueStatuses.Declined,
			LabelIDs:   []uint64{labelTask.ID, labelBug.ID},
		}),
		suite.makeIssue(suite.users.Kopatych, &makeIssueOptions{
			RepoID:     repoID,
			Title:      "Second issue",
			Visibility: entities.IssueVisibilities.Private,
			Priority:   entities.IssuePriorities.Critical,
			LabelIDs:   []uint64{labelFeature.ID},
		}),
		suite.makeIssue(suite.users.Krosh, &makeIssueOptions{
			RepoID:      repoID,
			Title:       "Third issue",
			Visibility:  entities.IssueVisibilities.Private,
			MilestoneID: &milestone.ID,
		}),
	}
	deletedIssue := suite.makeIssue(suite.users.Krosh, &makeIssueOptions{
		RepoID:     repoID,
		Title:      "Deleted issue",
		Visibility: entities.IssueVisibilities.Public,
	})
	err = suite.IssueRepo.Delete(context.Background(), deletedIssue.ID)
	require.NoError(t, err)

	// issue from another repository
	suite.makeIssue(suite.users.Krosh, &makeIssueOptions{
		RepoID:     suite.repos.Alpha.ID,
		Title:      "Alpha issue",
		Visibility: entities.IssueVisibilities.Public,
	})

	suite.makeIssue(suite.users.Kopatych, &makeIssueOptions{
		RepoID:     suite.repos.Alpha.ID,
		Title:      "Alpha issue by kopatych",
		Visibility: entities.IssueVisibilities.Public,
	})

	suite.makeIssue(suite.users.Krosh, &makeIssueOptions{
		RepoID:     suite.repos.Alpha.ID,
		AssigneeID: &suite.users.Kopatych.ID,
		Title:      "Alpha issue assigned to kopatych",
		Visibility: entities.IssueVisibilities.Public,
	})

	suite.makeIssue(suite.users.Krosh, &makeIssueOptions{
		RepoID:     suite.repos.Alpha.ID,
		AssigneeID: &suite.users.Kopatych.ID,
		Title:      "Alpha issue assigned to but it is private",
		Visibility: entities.IssueVisibilities.Private,
	})

	_ = issues

	pathIgnore := []string{"**/updated_at", "**/created_at", "**/id", "**/started_at", "**/completed_at"}

	t.Run("list issues: authorized user", func(t *testing.T) {
		resp, err := suite.gwClient.
			As(suite.users.Kopatych.Identity).
			Get("/me/issues")
		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, resp, pathIgnore...)
	})

	t.Run("list issues: repo slug", func(t *testing.T) {
		resp, err := suite.gwClient.As(suite.users.Kopatych.Identity).Get(fmt.Sprintf("/repos/%s/%s/issues", orgSlug, repoSlug))
		require.NoError(t, err)

		yarequire.HTTPCompareWithFixture(t, resp, pathIgnore...)
	})

	t.Run("list issues: filter", func(t *testing.T) {
		resp, err := suite.gwClient.As(suite.users.Kopatych.Identity).SetQueryParams(map[string]string{
			"filter": `priority=critical`,
		}).Get(fmt.Sprintf("/repos/%s/%s/issues", orgSlug, repoSlug))
		require.NoError(t, err)

		yarequire.HTTPCompareWithFixture(t, resp, pathIgnore...)
	})

	t.Run("list issues: filter by status", func(t *testing.T) {
		resp, err := suite.gwClient.As(suite.users.Kopatych.Identity).SetQueryParams(map[string]string{
			"filter": `status=declined`,
		}).Get(fmt.Sprintf("/repos/%s/%s/issues", orgSlug, repoSlug))
		require.NoError(t, err)

		yarequire.HTTPCompareWithFixture(t, resp, pathIgnore...)
	})

	t.Run("list issues: repo via id", func(t *testing.T) {
		resp, err := suite.gwClient.As(suite.users.Kopatych.Identity).Get(fmt.Sprintf("/repos/id:%s/issues", repo.UUID))
		require.NoError(t, err)

		yarequire.HTTPCompareWithFixture(t, resp, pathIgnore...)
	})

	t.Run("list issues: pagination", func(t *testing.T) {
		resp, err := suite.gwClient.As(suite.users.Kopatych.Identity).SetQueryParams(map[string]string{
			"page_size": "1",
		}).Get(fmt.Sprintf("/repos/%s/%s/issues", orgSlug, repoSlug))
		require.NoError(t, err)

		issuesResp, err := yarequire.GetArrayFromJSON(resp.Body(), "issues")
		require.NoError(t, err)
		issueTitle, err := yarequire.GetStringFromJSON(issuesResp[0], "title")
		require.NoError(t, err)
		require.Equal(t, issues[0].Title, issueTitle)
		nextPageToken, err := yarequire.GetStringFromJSON(resp.Body(), "next_page_token")
		require.NoError(t, err)

		resp, err = suite.gwClient.As(suite.users.Kopatych.Identity).SetQueryParams(map[string]string{
			"page_token": nextPageToken,
			"page_size":  "1",
		}).Get(fmt.Sprintf("/repos/%s/%s/issues", orgSlug, repoSlug))
		require.NoError(t, err)

		issuesResp, err = yarequire.GetArrayFromJSON(resp.Body(), "issues")
		require.NoError(t, err)
		issueTitle, err = yarequire.GetStringFromJSON(issuesResp[0], "title")
		require.NoError(t, err)
		require.Equal(t, issues[1].Title, issueTitle)
	})

	t.Run("create (defaults)", func(t *testing.T) {
		resp, err := suite.gwClient.As(suite.users.Kopatych.Identity).
			SetBody(pbPub.CreateIssueBody{
				Title:       "My new issue",
				Description: "This is my issue",
			}).
			Post(fmt.Sprintf("/repos/%s/%s/issues", orgSlug, repoSlug))

		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, resp, pathIgnore...)
	})

	t.Run("create (explicit)", func(t *testing.T) {
		resp, err := suite.gwClient.As(suite.users.Kopatych.Identity).
			SetBody(pbPub.CreateIssueBody{
				Title:       "My new issue",
				Description: "This is my issue",
				Visibility:  pbPub.Issue_private,
				Priority:    pbPub.Issue_blocker,
				StatusSlug:  entities.IssueStatuses.InProgress.Slug,
				LabelIds:    []string{grpc_marshalling.UUIDInverse(labelBug.UUID), grpc_marshalling.UUIDInverse(labelFeature.UUID)},
			}).
			Post(fmt.Sprintf("/repos/%s/%s/issues", orgSlug, repoSlug))

		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, resp, pathIgnore...)
	})
	t.Run("update", func(t *testing.T) {
		resp, err := suite.gwClient.As(suite.users.Kopatych.Identity).
			SetBody(pbPub.CreateIssueBody{
				Title:       "OLD",
				Description: "OLD",
			}).
			Post(fmt.Sprintf("/repos/%s/%s/issues", orgSlug, repoSlug))
		require.NoError(t, err)

		pk, err := yarequire.GetIDFromJSON(resp.Body(), "slug")
		require.NoError(t, err)

		resp, err = suite.gwClient.As(suite.users.Kopatych.Identity).
			SetBody(pbPub.UpdateIssueBody{
				Title:       "NEW1",
				Description: "NEW2",
				StatusSlug:  entities.IssueStatuses.Closed.Slug,
				Priority:    pbPub.Issue_critical,
				AssigneeId:  grpc_marshalling.UUIDInverse(suite.users.Krosh.UUID),
				Visibility:  pbPub.Issue_private,
				Deadline:    grpc.TimeToProtocTs(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)),
			}).
			Patch(fmt.Sprintf("/repos/%s/%s/issues/%d", orgSlug, repoSlug, pk))

		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, resp, pathIgnore...)
	})

	t.Run("update by id", func(t *testing.T) {
		resp, err := suite.gwClient.As(suite.users.Kopatych.Identity).
			SetBody(pbPub.CreateIssueBody{
				Title:       "OLD2",
				Description: "OLD2",
			}).
			Post(fmt.Sprintf("/repos/id:%s/issues", repo.UUID.String()))
		require.NoError(t, err)

		uuid, err := yarequire.GetUUIDFromJSON(resp.Body(), "id")
		require.NoError(t, err)

		resp, err = suite.gwClient.As(suite.users.Kopatych.Identity).
			SetBody(pbPub.UpdateIssueBody{
				Title:       "NEW2",
				Description: "NEW2",
				StatusSlug:  entities.IssueStatuses.Closed.Slug,
				Priority:    pbPub.Issue_critical,
				AssigneeId:  grpc_marshalling.UUIDInverse(suite.users.Krosh.UUID),
				Visibility:  pbPub.Issue_private,
			}).
			Patch(fmt.Sprintf("/issues/id:%s", uuid.String()))

		require.NoError(t, err)
		yarequire.HTTPCompareWithFixture(t, resp, pathIgnore...)
	})

	t.Run("milestone", func(t *testing.T) {
		milestone := suite.makeMilestone(suite.users.Kopatych, &makeMilestoneOptions{
			RepoID: repoID,
			Name:   "Milestone",
		})
		oldMilestoneID := grpc_marshalling.UUIDInverse(milestone.UUID)
		milestoneNew := suite.makeMilestone(suite.users.Kopatych, &makeMilestoneOptions{
			RepoID: repoID,
			Name:   "MilestoneOld",
		})

		resp, err := suite.gwClient.As(suite.users.Kopatych.Identity).
			SetBody(pbPub.CreateIssueBody{
				Title:       "Super Issue",
				Description: "Super Issue",
				MilestoneId: &oldMilestoneID,
			}).
			Post(fmt.Sprintf("/repos/%s/%s/issues", orgSlug, repoSlug))
		require.NoError(t, err)

		mpk, err := yarequire.GetUUIDFromJSON(resp.Body(), "milestone.id")
		require.NoError(t, err)
		require.Equal(t, milestone.UUID, mpk)

		pk, err := yarequire.GetIDFromJSON(resp.Body(), "slug")
		require.NoError(t, err)

		resp, err = suite.gwClient.As(suite.users.Kopatych.Identity).
			SetBody(pbPub.UpdateIssueBody{
				MilestoneId: grpc_marshalling.UUIDInverse(milestoneNew.UUID),
			}).
			Patch(fmt.Sprintf("/repos/%s/%s/issues/%d", orgSlug, repoSlug, pk))

		require.NoError(t, err)

		mpk, err = yarequire.GetUUIDFromJSON(resp.Body(), "milestone.id")
		require.NoError(t, err)
		require.Equal(t, milestoneNew.UUID, mpk)
	})

	t.Run("delete", func(t *testing.T) {
		resp, err := suite.gwClient.As(suite.users.Kopatych.Identity).
			SetBody(pbPub.CreateIssueBody{
				Title:       "deleteme",
				Description: "deleteme",
			}).
			Post(fmt.Sprintf("/repos/%s/%s/issues", orgSlug, repoSlug))
		require.NoError(t, err)

		pk, err := yarequire.GetIDFromJSON(resp.Body(), "slug")
		require.NoError(t, err)

		resp, err = suite.gwClient.As(suite.users.Kopatych.Identity).
			Delete(fmt.Sprintf("/repos/%s/%s/issues/%d", orgSlug, repoSlug, pk))

		yarequire.StatusCode(t, resp, err, 204)

		resp, err = suite.gwClient.As(suite.users.Kopatych.Identity).
			Get(fmt.Sprintf("/repos/%s/%s/issues/%d", orgSlug, repoSlug, pk))

		yarequire.StatusCode(t, resp, err, 404)
	})

	t.Run("delete by id", func(t *testing.T) {
		resp, err := suite.gwClient.As(suite.users.Kopatych.Identity).
			SetBody(pbPub.CreateIssueBody{
				Title:       "deleteme",
				Description: "deleteme",
			}).
			Post(fmt.Sprintf("/repos/id:%s/issues", repo.UUID.String()))
		require.NoError(t, err)

		uuid, err := yarequire.GetUUIDFromJSON(resp.Body(), "id")
		require.NoError(t, err)

		resp, err = suite.gwClient.As(suite.users.Kopatych.Identity).
			Delete(fmt.Sprintf("/issues/id:%s", uuid.String()))

		yarequire.StatusCode(t, resp, err, 204)

		resp, err = suite.gwClient.As(suite.users.Kopatych.Identity).
			Get(fmt.Sprintf("/issues/id:%s", uuid.String()))

		yarequire.StatusCode(t, resp, err, 404)
	})

	t.Run("get", func(t *testing.T) {
		resp, err := suite.gwClient.As(suite.users.Kopatych.Identity).
			Get(fmt.Sprintf("/repos/%s/%s/issues/%d", orgSlug, repoSlug, issues[0].PublicID))
		require.NoError(t, err)

		resp2, err := suite.gwClient.As(suite.users.Kopatych.Identity).Get(fmt.Sprintf("/issues/id:%s", issues[0].UUID.String()))
		require.NoError(t, err)

		yarequire.HTTPCompareWithFixture(t, resp, pathIgnore...)
		require.Equal(t, resp.Body(), resp2.Body())
	})

}

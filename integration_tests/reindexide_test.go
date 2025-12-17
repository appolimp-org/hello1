package integrationtests

import (
	"fmt"
	"gitcore/internal/housekeeping"
	pb "private_api/generated/yandex/cloud/priv/ide/v1/gitsyncer"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func (suite *RwApiTestSuite) TestReindexIDE() {
	t := suite.T()

	taskDeps := housekeeping.ReindexIDEParams{
		Organizations: suite.OrgRepo,
		Repos:         suite.RepoRepo,
		IDEService:    suite.ideService,
	}

	task := housekeeping.NewReindexIDECmd(taskDeps)

	t.Run("dry run", func(t *testing.T) {
		suite.ideService.ClearSentEvents()
		err := task.Invoke("-dry_run=true")
		require.NoError(t, err)
		evnts := suite.ideService.SentEvents()
		require.Len(t, evnts, 0)
	})
	t.Run("reindex", func(t *testing.T) {
		suite.ideService.ClearSentEvents()
		err := task.Invoke("-batch_interval=0")
		require.NoError(t, err)
		evnts := suite.ideService.SentEvents()
		require.True(t, len(evnts) > 0)

		// check some of the repos from the test fixture
		require.True(t, suite.repoReindexed(evnts, suite.repos.Alpha.ID))
		require.True(t, suite.repoReindexed(evnts, suite.repos.History.ID))
		require.True(t, suite.repoReindexed(evnts, suite.repos.Blame.ID))
		require.True(t, suite.repoReindexed(evnts, suite.repos.BigDiff.ID))
		require.True(t, suite.repoReindexed(evnts, suite.repos.MergeBase.ID))
	})
	t.Run("reindex from repo id", func(t *testing.T) {
		suite.ideService.ClearSentEvents()
		err := task.Invoke("-batch_interval=0", fmt.Sprintf("-start_from_repo_id=%d", suite.repos.Alpha.ID+1))
		require.NoError(t, err)
		evnts := suite.ideService.SentEvents()
		require.True(t, len(evnts) > 0)

		require.False(t, suite.repoReindexed(evnts, suite.repos.Alpha.ID))
	})
	t.Run("reindex a specific repo", func(t *testing.T) {
		suite.ideService.ClearSentEvents()
		err := task.Invoke(fmt.Sprintf("-repo_id=%d", suite.repos.Alpha.ID))
		require.NoError(t, err)
		evnts := suite.ideService.SentEvents()
		require.Len(t, evnts, 1)

		require.True(t, suite.repoReindexed(evnts, suite.repos.Alpha.ID))
	})
}

func (suite *RwApiTestSuite) repoReindexed(evnts []*pb.HandleEventRequest, repoID uint64) bool {
	return slices.ContainsFunc(evnts, func(eventRequest *pb.HandleEventRequest) bool {
		return eventRequest.Event.Repository.Id == repoID
	})
}

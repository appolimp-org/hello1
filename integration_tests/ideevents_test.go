package integrationtests

import (
	"common/utils/rolesgenerator/iam"
	"github.com/go-git/go-git/v5/plumbing"
	"testing"

	"github.com/stretchr/testify/require"

	"gitcore/internal/entities"
	pb "private_api/generated/yandex/cloud/priv/ide/v1/gitsyncer"
)

func (suite *RwApiTestSuite) TestIDEOnPushEvents() {
	suite.T().Run("Push updates branch", func(t *testing.T) {
		suite.WaitForWorkflows(t, entities.WorkflowTypes.BroadcastRepoPushEvent)
		suite.addRole(t, suite.users.Pikachu, suite.repos.ListBranches, iam.Roles.RepositoriesDeveloper)

		suite.ideService.ClearSentEvents()
		suite.addRole(suite.T(), suite.users.Pikachu, suite.repos.ListBranches, iam.Roles.RepositoriesDeveloper)
		suite.primitivePush(
			t, suite.users.Pikachu, suite.repos.ListBranches,
			plumbing.NewBranchReferenceName("branch"), true,
		)
		suite.WaitForWorkflows(t, entities.WorkflowTypes.BroadcastRepoPushEvent)

		evnts := suite.ideService.SentEvents()
		require.Equal(t, 2, len(evnts))
		event := evnts[1].Event

		require.Equal(t, pb.Event_PUSH_EVENT, event.EventType)
		require.Equal(t, suite.repos.ListBranches.ID, event.Repository.Id)
	})
}

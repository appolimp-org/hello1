package integrationtests

import (
	"common/oyaml"
	"context"
	"gitcore/internal/entities"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"github.com/stretchr/testify/require"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
)

func (suite *RwApiTestSuite) TestPushAnnotatedTags() {
	t := suite.T()
	ctx := context.Background()
	user := suite.users.Admin

	repo, _ := suite.createRepo(t,
		user,
		"test-repo-annotated-tag",
		pb.ResourceVisibility_RESOURCE_PUBLIC,
		grpc_marshalling.IDInverse(suite.orgs.Yandex.ID))

	repoID, err := grpc_marshalling.IDDirect(repo.Id)
	require.NoError(t, err)

	repoEntry, err := suite.RepoRepo.GetRepositoryByID(ctx, repoID)
	require.NoError(t, err)

	suite.addDefaultOYaml(repoEntry, "master", oyaml.CIPath)
	_ = repoEntry

	suite.addCommits(t, repo.CloneUrl.Https, user, commitsOptions{})

	cgc, err := suite.clone(t, repo.CloneUrl.Https, user)
	require.NoError(t, err)

	cgc.Must(t, "tag", "-a", "anntag", "-m", "msg", "HEAD")
	cgc.Must(t, "push", "origin", "--tags")

	// clear events sent to CI if any
	suite.WaitForWorkflows(t, entities.WorkflowTypes.SendCIPushTriggers)
	evts := suite.ciService.SentEvents()
	_ = evts
	suite.ciService.ClearSentEvents()
}

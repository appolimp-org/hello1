package integrationtests

import (
	"common/utils"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/testutils"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"testing"

	"github.com/stretchr/testify/require"

	"gitcore/internal/entities"
)

func (suite *RwApiTestSuite) TestListContributors() {
	t := suite.T()

	client := pb.NewRepoServiceClient(suite.grpcClient)

	type ListContributorsTest struct {
		name         string
		addUsers     []*entities.User
		contributors []uint64
	}

	tests := []ListContributorsTest{
		{
			name:         "list almost empty repo",
			addUsers:     []*entities.User{},
			contributors: []uint64{suite.users.Admin.ID},
		},
		{
			name: "list added contributors",
			addUsers: []*entities.User{
				suite.users.Admin,
				suite.users.Kopatych,
				suite.users.Barash,
			},
			contributors: []uint64{
				suite.users.Admin.ID,
				suite.users.Kopatych.ID,
				suite.users.Barash.ID,
			},
		},
	}

	ctx := testutils.AuthorizeGRPC(suite.users.Admin.Identity)

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := suite.ImportRepo(suite.orgs.Yandex, test.name, testutils.BasicRepo, nil)

			repoID := grpc_marshalling.IDInverse(repo.ID)

			for _, user := range test.addUsers {
				suite.RepoContributorRepo.AddContributor(ctx, repo.ID, user.ID)
			}

			listResp, err := client.ListContributors(ctx, &pb.ListContributorsRequest{
				RepoId:   repoID,
				PageSize: utils.PtrFromValue(uint64(25)),
			})

			require.NoError(t, err)

			require.NotNil(t, listResp)

			IDs := make([]uint64, len(listResp.UserIds))
			for i, IDString := range listResp.UserIds {
				ID, err := grpc_marshalling.IDDirect(IDString)
				require.NoError(t, err)
				IDs[i] = ID
			}

			require.ElementsMatch(t, test.contributors, IDs)
		})
	}
}

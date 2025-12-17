package integrationtests

import (
	"common/grpc"
	"common/oyaml"
	"common/testutils/yarequire"
	"common/utils"
	"common/utils/rolesgenerator/iam"
	"fmt"
	"gitcore/internal/entities"
	except "gitcore/internal/exceptions"
	"gitcore/internal/testutils"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"testing"

	"github.com/stretchr/testify/require"
)

func (suite *RwApiTestSuite) TestGrpcPRChangeStatus() {
	for _, configPath := range []string{oyaml.ReviewPath, oyaml.OldPath} {
		suite.BeforeTest("", "")

		t := suite.T()

		client := pb.NewPRServiceClient(suite.grpcClient)
		ctx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)

		suite.addRole(t, suite.users.Krosh, suite.repos.Alpha, iam.Roles.RepositoriesMaintainer)
		suite.addRole(t, suite.users.Kopatych, suite.repos.Alpha, iam.Roles.RepositoriesAdmin)

		// empty old config to check newer is preferred
		suite.mustBash(suite.repos.Alpha, fmt.Sprintf(`
		git checkout master
		echo -n "" > %s
		mkdir %s
		echo "
codereview:
  need_ships: 0
  auto_assign: false" > %s
		git add .
		git commit -m "Disabled codereview"
	`, oyaml.OldPath, oyaml.SourceCraftDirectory, configPath))

		pr := suite.makePullRequest(suite.users.Krosh, &makePrOptions{
			Repo:    suite.repos.Alpha,
			Title:   "TASK-1 add README.md",
			Source:  "branch",
			Target:  "master",
			Publish: utils.PtrFromValue(false),
		})

		type transition struct {
			name string
			fn   func() error
		}

		discard := transition{
			name: "discard",
			fn:   func() error { _, err := client.Discard(ctx, &pb.DiscardRequest{Id: grpc.MarshalID(pr.ID)}); return err },
		}

		publish := transition{
			name: "publish",
			fn:   func() error { _, err := client.Publish(ctx, &pb.PublishRequest{Id: grpc.MarshalID(pr.ID)}); return err },
		}

		draft := transition{
			name: "draft",
			fn:   func() error { _, err := client.Draft(ctx, &pb.DraftRequest{Id: grpc.MarshalID(pr.ID)}); return err },
		}

		reopen := transition{
			name: "reopen",
			fn:   func() error { _, err := client.Reopen(ctx, &pb.ReopenRequest{Id: grpc.MarshalID(pr.ID)}); return err },
		}

		abort := transition{
			name: "abort",
			fn:   func() error { _, err := client.Abort(ctx, &pb.AbortRequest{Id: grpc.MarshalID(pr.ID)}); return err },
		}

		merge := transition{
			name: "merge",
			fn: func() error {
				_, err := client.Merge(ctx, &pb.MergeRequest{Force: true, PrId: grpc.MarshalID(pr.ID)})
				return err
			},
		}

		// from -> to
		smCases := map[entities.PRStatus][]struct {
			expected   *entities.PRStatus
			transition transition
		}{
			entities.PRStatuses.Draft: {
				{transition: draft},
				{transition: discard, expected: &entities.PRStatuses.Discarded},
				{transition: publish, expected: &entities.PRStatuses.Open},
				{transition: reopen},
				{transition: abort},
				{transition: merge},
			},

			entities.PRStatuses.Open: {
				{transition: draft, expected: &entities.PRStatuses.Draft},
				{transition: discard, expected: &entities.PRStatuses.Discarded},
				{transition: publish},
				{transition: reopen},
				{transition: abort},
				//{transition: merge}, TODO: write me
			},

			entities.PRStatuses.Merging: {
				{transition: draft},
				{transition: discard},
				{transition: publish},
				{transition: reopen},
				{transition: merge},
				//{transition: abort, expected: &entities.PRStatuses.Open},  TODO: write me
			},

			entities.PRStatuses.Discarded: {
				{transition: draft},
				{transition: discard},
				{transition: publish},
				{transition: reopen, expected: &entities.PRStatuses.Open},
				{transition: abort},
				{transition: merge},
			},

			entities.PRStatuses.Merged: {
				{transition: draft},
				{transition: discard},
				{transition: publish},
				{transition: reopen},
				{transition: abort},
				{transition: merge},
			},
		}

		for stateFrom, tcc := range smCases {
			for _, tc := range tcc {
				name := fmt.Sprintf("invalid transition: verb %s; state %s", tc.transition.name, stateFrom)
				if tc.expected != nil {
					name = fmt.Sprintf("transition: verb %s; states %s->%v", tc.transition.name, stateFrom, *tc.expected)
				}

				t.Run(name, func(t *testing.T) {
					err := suite.PullRequestRepo.UpdateTable(ctx, pr.ID, map[string]interface{}{"status": stateFrom})
					require.NoError(t, err)

					err = tc.transition.fn()
					if tc.expected != nil {
						require.NoError(t, err)
						prCheck, err := suite.PullRequestRepo.Get(ctx, pr.ID)
						require.NoError(t, err)
						require.Equal(t, *tc.expected, prCheck.Status)
					} else {
						yarequire.ProtoExceptionTemplate(t, err, except.WrongPRStatusTransition)
					}
				})
			}
		}

		suite.AfterTest("", "")
	}
}

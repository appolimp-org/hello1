package integrationtests

import (
	"common/functools"
	"common/grpc"
	"common/testutils/yarequire"
	"common/utils"
	"common/utils/rolesgenerator/iam"
	"context"
	"gitcore/internal/entities"
	except "gitcore/internal/exceptions"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/testutils"
	"gitcore/pkg/pagination"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/testing/protocmp"
)

func (suite *RwApiTestSuite) TestGrpcRepoReactions_RateRepo() {
	t := suite.T()
	client := pb.NewRatingServiceClient(suite.grpcClient)
	repo := suite.repos.Alpha
	anonynousCtx := context.Background()

	addReactionAndCheck := func(t *testing.T, user *entities.User, reaction entities.RatingReactionType, expectReactions []pb.RatingReactionType) {
		suite.addRole(t, user, repo, iam.Roles.RepositoriesViewer)
		ctx := testutils.AuthorizeGRPC(user.Identity)

		_, err := client.RateRepo(ctx, &pb.RateRepoRequest{
			RepoId:   grpc.MarshalID(repo.ID),
			Reaction: grpc_marshalling.RatingReactionTypeInverse(reaction),
		})
		require.NoError(t, err)

		userReactions, err := client.ListMyReactions(ctx, &pb.ListMyRatingReactionsRequest{})
		require.NoError(t, err)
		require.Len(t, userReactions.Reactions, len(expectReactions))
		for i, r := range userReactions.Reactions {
			require.Equal(t, expectReactions[i], r.Reaction)
		}

		userRepoReaction, err := client.GetMyRepoReaction(ctx, &pb.GetMyRepoReactionRequest{
			RepoId: grpc.MarshalID(repo.ID),
		})
		require.NoError(t, err)
		if len(expectReactions) == 0 {
			require.Equal(t, pb.RatingReactionType_RR_TYPE_NEUTRAL, userRepoReaction.Reaction)
		} else {
			require.Equal(t, expectReactions[0], userRepoReaction.Reaction)
		}
	}
	t.Run("anonymous can not add or remove reaction on public repo", func(t *testing.T) {
		r := suite.RepoRepo.UpdateRepositoryByID(suite.repos.MergeBase.ID)
		r.SetRepoVisibility(entities.Visibilities.Public)
		err := r.Commit(context.Background())
		require.NoError(t, err)

		_, err = client.RateRepo(anonynousCtx, &pb.RateRepoRequest{
			RepoId:   grpc.MarshalID(repo.ID),
			Reaction: grpc_marshalling.RatingReactionTypeInverse(entities.RatingReactionTypes.PositiveLevel20),
		})

		yarequire.ProtoStatusEqual(t, codes.Unauthenticated, err)
	})

	t.Run("add reaction", func(t *testing.T) {
		addReactionAndCheck(t, suite.users.Krosh, entities.RatingReactionTypes.PositiveLevel20, []pb.RatingReactionType{
			pb.RatingReactionType_RR_TYPE_POSITIVE_LEVEL_20,
		})
	})

	t.Run("add and delete reaction", func(t *testing.T) {
		addReactionAndCheck(t, suite.users.Barash, entities.RatingReactionTypes.PositiveLevel10, []pb.RatingReactionType{
			pb.RatingReactionType_RR_TYPE_POSITIVE_LEVEL_10,
		})
		addReactionAndCheck(t, suite.users.Barash, entities.RatingReactionTypes.Neutral, []pb.RatingReactionType{})
	})

	t.Run("add and change reaction", func(t *testing.T) {
		addReactionAndCheck(t, suite.users.Kopatych, entities.RatingReactionTypes.PositiveLevel30, []pb.RatingReactionType{
			pb.RatingReactionType_RR_TYPE_POSITIVE_LEVEL_30,
		})
		addReactionAndCheck(t, suite.users.Kopatych, entities.RatingReactionTypes.PositiveLevel10, []pb.RatingReactionType{
			pb.RatingReactionType_RR_TYPE_POSITIVE_LEVEL_10,
		})
	})

	repoReactions, err := suite.RatingReactionsService.ListByEntity(context.Background(), entities.EntityTypes.Repository, repo.ID, pagination.Options{}, nil)
	require.NoError(t, err)
	require.Len(t, repoReactions.Result, 2)
	for _, star := range repoReactions.Result {
		require.Equal(t, entities.EntityTypes.Repository, star.EntityType)
		require.Equal(t, repo.ID, star.EntityID)
	}

	require.Equal(t, suite.users.Kopatych.ID, repoReactions.Result[0].UserID)
	require.Equal(t, entities.RatingReactionTypes.PositiveLevel10, repoReactions.Result[0].Reaction)

	require.Equal(t, suite.users.Krosh.ID, repoReactions.Result[1].UserID)
	require.Equal(t, repo.ID, repoReactions.Result[1].EntityID)

}

func (suite *RwApiTestSuite) TestGrpcRepoRating_ListMyReactions() {
	t := suite.T()
	client := pb.NewRatingServiceClient(suite.grpcClient)
	user := suite.users.Kopatych
	ctx := testutils.AuthorizeGRPC(user.Identity)

	repoReactions := []struct {
		repo     *entities.Repository
		reaction entities.RatingReactionType
	}{
		{repo: suite.repos.Alpha, reaction: entities.RatingReactionTypes.PositiveLevel20},
		{repo: suite.repos.History, reaction: entities.RatingReactionTypes.PositiveLevel10},
		{repo: suite.repos.AlphaFork, reaction: entities.RatingReactionTypes.PositiveLevel30},
		{repo: suite.repos.Blame, reaction: entities.RatingReactionTypes.PositiveLevel30},
		{repo: suite.repos.History, reaction: entities.RatingReactionTypes.Neutral},
		{repo: suite.repos.Crisscross, reaction: entities.RatingReactionTypes.PositiveLevel20},
		{repo: suite.repos.BranchPolicy, reaction: entities.RatingReactionTypes.PositiveLevel10},
	}

	for _, repoReaction := range repoReactions {
		suite.addRole(t, user, repoReaction.repo, iam.Roles.RepositoriesViewer)
		_, err := client.RateRepo(ctx, &pb.RateRepoRequest{
			RepoId:   grpc.MarshalID(repoReaction.repo.ID),
			Reaction: grpc_marshalling.RatingReactionTypeInverse(repoReaction.reaction),
		})
		require.NoError(t, err)

		reaction, err := client.GetMyRepoReaction(ctx, &pb.GetMyRepoReactionRequest{
			RepoId: grpc.MarshalID(repoReaction.repo.ID),
		})
		require.NoError(t, err)
		require.Equal(t, grpc_marshalling.RatingReactionTypeInverse(repoReaction.reaction), reaction.Reaction)
	}

	checkOrdering := func(t *testing.T, reactions []*pb.MyRatingReaction) {
		for i := 1; i < len(reactions); i++ {
			require.Less(t,
				reactions[i].CreatedAt.AsTime().UnixNano(),
				reactions[i-1].CreatedAt.AsTime().UnixNano(),
			)
		}
	}

	t.Run("no filter", func(t *testing.T) {
		response, err := client.ListMyReactions(ctx, &pb.ListMyRatingReactionsRequest{})
		require.NoError(t, err)
		require.Len(t, response.Reactions, 5)
		checkOrdering(t, response.Reactions)

		//yarequire.ProtoDumpFixture(t, response)
		yarequire.ProtoCompareWithFixture(t, response, protocmp.IgnoreFields(&pb.MyRatingReaction{}, "created_at"))
	})

	t.Run("filter by reaction", func(t *testing.T) {
		response, err := client.ListMyReactions(ctx, &pb.ListMyRatingReactionsRequest{
			Filter: utils.PtrFromValue(pb.RatingReactionType_RR_TYPE_POSITIVE_LEVEL_20),
		})
		require.NoError(t, err)
		require.Len(t, response.Reactions, 2)
		require.Equal(t, grpc.MarshalID(suite.repos.Crisscross.ID), response.Reactions[0].RepoId)
		require.Equal(t, grpc.MarshalID(suite.repos.Alpha.ID), response.Reactions[1].RepoId)

		checkOrdering(t, response.Reactions)

		//yarequire.ProtoDumpFixture(t, response)
		yarequire.ProtoCompareWithFixture(t, response, protocmp.IgnoreFields(&pb.MyRatingReaction{}, "created_at"))
	})

	t.Run("filter by query", func(t *testing.T) {
		response, err := client.ListMyReactions(ctx, &pb.ListMyRatingReactionsRequest{
			Query: "alpha",
		})
		require.NoError(t, err)
		require.Len(t, response.Reactions, 2)
		require.Equal(t, grpc.MarshalID(suite.repos.AlphaFork.ID), response.Reactions[0].RepoId)
		require.Equal(t, grpc.MarshalID(suite.repos.Alpha.ID), response.Reactions[1].RepoId)

		checkOrdering(t, response.Reactions)

		//yarequire.ProtoDumpFixture(t, response)
		yarequire.ProtoCompareWithFixture(t, response, protocmp.IgnoreFields(&pb.MyRatingReaction{}, "created_at"))
	})

	t.Run("filter by reaction and query", func(t *testing.T) {
		response, err := client.ListMyReactions(ctx, &pb.ListMyRatingReactionsRequest{
			Filter: utils.PtrFromValue(pb.RatingReactionType_RR_TYPE_POSITIVE_LEVEL_20),
			Query:  "alpha",
		})
		require.NoError(t, err)
		require.Len(t, response.Reactions, 1)
		require.Equal(t, grpc.MarshalID(suite.repos.Alpha.ID), response.Reactions[0].RepoId)
	})
}

func (suite *RwApiTestSuite) TestGrpcRepoRatingChange() {
	t := suite.T()
	client := pb.NewRatingServiceClient(suite.grpcClient)
	repo := suite.repos.Alpha

	for _, action := range []struct {
		user           *entities.User
		reaction       entities.RatingReactionType
		expectedRating float64
	}{
		{
			user:           suite.users.Kopatych,
			reaction:       entities.RatingReactionTypes.PositiveLevel10,
			expectedRating: 1.0,
		},
		{
			user:           suite.users.Krosh,
			reaction:       entities.RatingReactionTypes.PositiveLevel20,
			expectedRating: 3.5,
		},
		{
			user:           suite.users.Kopatych,
			reaction:       entities.RatingReactionTypes.PositiveLevel30,
			expectedRating: 7.5,
		},
		{
			user:           suite.users.Krosh,
			reaction:       entities.RatingReactionTypes.Neutral,
			expectedRating: 5.0,
		},
		{
			user:           suite.users.Barash,
			reaction:       entities.RatingReactionTypes.PositiveLevel10,
			expectedRating: 6.0,
		},
		{
			user:           suite.users.Pikachu,
			reaction:       entities.RatingReactionTypes.PositiveLevel20,
			expectedRating: 8.5,
		},
		{
			user:           suite.users.Barash,
			reaction:       entities.RatingReactionTypes.Neutral,
			expectedRating: 7.5,
		},
		{
			user:           suite.users.Pikachu,
			reaction:       entities.RatingReactionTypes.PositiveLevel10,
			expectedRating: 6.0,
		},
	} {
		ctx := testutils.AuthorizeGRPC(action.user.Identity)
		suite.addRole(t, action.user, repo, iam.Roles.Viewer)

		_, err := client.RateRepo(ctx, &pb.RateRepoRequest{
			RepoId:   grpc.MarshalID(repo.ID),
			Reaction: grpc_marshalling.RatingReactionTypeInverse(action.reaction),
		})
		require.NoError(t, err)

		rating, err := suite.RatingRepo.Get(ctx, repo.ID)
		require.NoError(t, err)
		require.Equal(t, action.expectedRating, rating.Rating)
	}
}

func (suite *RwApiTestSuite) TestRatingRepo_RepoDeletion() {
	t := suite.T()
	ctx := context.Background()
	user := suite.users.Kopatych
	orgID := grpc.MarshalID(suite.orgs.Yandex.ID)

	t.Run("soft delete", func(t *testing.T) {
		suite.addOrgRole(t, user, suite.orgs.Yandex, iam.Roles.Admin)

		repo, _ := suite.createRepo(t, user, "test-rating-soft-delete", pb.ResourceVisibility_RESOURCE_PUBLIC, orgID)
		repoID, err := grpc.ParseID(repo.Id)
		require.NoError(t, err)

		err = suite.RatingRepo.Replace(ctx, &entities.RepositoryRating{
			RepoID:    repoID,
			Rating:    10.0,
			UpdatedAt: time.Now(),
		})
		require.NoError(t, err)

		require.NoError(t, suite.RepoService.DeleteRepository(ctx, repoID, user.ID))

		_, err = suite.RatingRepo.Get(ctx, repoID)
		require.ErrorIs(t, err, except.EntityNotFound)
	})

	t.Run("hard delete", func(t *testing.T) {
		suite.addOrgRole(t, user, suite.orgs.Yandex, iam.Roles.Admin)

		repo, _ := suite.createRepo(t, user, "test-rating-hard-delete", pb.ResourceVisibility_RESOURCE_PUBLIC, orgID)
		repoID, err := grpc.ParseID(repo.Id)
		require.NoError(t, err)

		err = suite.RatingRepo.Replace(ctx, &entities.RepositoryRating{
			RepoID:    repoID,
			Rating:    10.0,
			UpdatedAt: time.Now(),
		})
		require.NoError(t, err)

		require.NoError(t, suite.RepoRepo.HardDelete(ctx, repoID))

		_, err = suite.RatingRepo.Get(ctx, repoID)
		require.ErrorIs(t, err, except.EntityNotFound)
	})
}

func (suite *RwApiTestSuite) TestGrpcRepoRatingPercentiles() {
	t := suite.T()
	user := suite.users.Kopatych
	org := suite.orgs.Yandex
	ctx := testutils.AuthorizeGRPC(user.Identity)
	suite.addOrgRole(t, user, org, iam.Roles.RepositoriesDeveloper)

	ratingClient := pb.NewRatingServiceClient(suite.grpcClient)
	repoClient := pb.NewRepoServiceClient(suite.grpcClient)

	makeRepoAndReact := func(t *testing.T, slug string, reaction pb.RatingReactionType) (pbRepoID string) {
		pbRepo, _ := suite.createRepo(t, user, slug, pb.ResourceVisibility_RESOURCE_PUBLIC, grpc.MarshalID(org.ID))
		_, err := ratingClient.RateRepo(ctx, &pb.RateRepoRequest{
			RepoId:   pbRepo.Id,
			Reaction: reaction,
		})
		require.NoError(t, err)

		return pbRepo.Id
	}

	requireRating := func(t *testing.T, pbRepoID string, percentile float32, stats map[pb.RatingReactionType]int32) {
		repo, err := repoClient.Get(ctx, &pb.GetRepositoryRequest{
			Repo: &pb.GetRepositoryRequest_Id{
				Id: pbRepoID,
			},
		})
		require.NoError(t, err)
		require.NotNil(t, repo.Rating)
		require.Equal(t, percentile, repo.Rating.Percentile)
		require.Len(t, repo.Rating.Reactions, len(stats))
		for r, c := range stats {
			require.Equal(t, c, repo.Rating.Reactions[r.String()])
		}
	}

	repoID := makeRepoAndReact(t, "test-repo-1", pb.RatingReactionType_RR_TYPE_POSITIVE_LEVEL_30)
	suite.WaitForCronJob(
		t,
		entities.CronJobs.RecalculateRepoRatingPercentiles,
		entities.WorkflowTypes.RecalculateRepoRatingPercentiles,
	)
	requireRating(t, repoID, 1.0, map[pb.RatingReactionType]int32{
		pb.RatingReactionType_RR_TYPE_POSITIVE_LEVEL_30: 1,
	})

	_, err := ratingClient.RateRepo(ctx, &pb.RateRepoRequest{
		RepoId:   repoID,
		Reaction: pb.RatingReactionType_RR_TYPE_NEUTRAL,
	}) // drop reaction
	require.NoError(t, err)
	repoID1 := makeRepoAndReact(t, "test-repo-2", pb.RatingReactionType_RR_TYPE_POSITIVE_LEVEL_10)
	repoID2 := makeRepoAndReact(t, "test-repo-3", pb.RatingReactionType_RR_TYPE_POSITIVE_LEVEL_20)
	repoID3 := makeRepoAndReact(t, "test-repo-4", pb.RatingReactionType_RR_TYPE_POSITIVE_LEVEL_30)

	suite.WaitForCronJob(
		t,
		entities.CronJobs.RecalculateRepoRatingPercentiles,
		entities.WorkflowTypes.RecalculateRepoRatingPercentiles,
	)

	requireRating(t, repoID, 100.0, map[pb.RatingReactionType]int32{})
	requireRating(t, repoID1, 75.0, map[pb.RatingReactionType]int32{
		pb.RatingReactionType_RR_TYPE_POSITIVE_LEVEL_10: 1,
	})
	requireRating(t, repoID2, 50.0, map[pb.RatingReactionType]int32{
		pb.RatingReactionType_RR_TYPE_POSITIVE_LEVEL_20: 1,
	})
	// percentiles behave weirdly when there are <100 repos, it's not an issue
	requireRating(t, repoID3, 1.0, map[pb.RatingReactionType]int32{
		pb.RatingReactionType_RR_TYPE_POSITIVE_LEVEL_30: 1,
	})
}

func (suite *RwApiTestSuite) TestGrpcRepoRatingPercentiles_NoRatings() {
	suite.WaitForCronJob(
		suite.T(),
		entities.CronJobs.RecalculateRepoRatingPercentiles,
		entities.WorkflowTypes.RecalculateRepoRatingPercentiles,
	)
}

func (suite *RwApiTestSuite) TestGrpcRepoRatingPercentiles_BeforeFirstRun() {
	t := suite.T()
	user := suite.users.Kopatych
	org := suite.orgs.Yandex
	ctx := testutils.AuthorizeGRPC(user.Identity)
	suite.addOrgRole(t, user, org, iam.Roles.RepositoriesDeveloper)

	// to ensure "no percentiles were calculated yet" even if workflow was run
	suite.CancelAllWorkflows()
	err := suite.RatingRepo.ClearPercentileThresholds(ctx)
	require.NoError(t, err)

	pbRepo1, _ := suite.createRepo(t, user, "test-repo-1", pb.ResourceVisibility_RESOURCE_PUBLIC, grpc.MarshalID(org.ID))
	pbRepo2, _ := suite.createRepo(t, user, "test-repo-2", pb.ResourceVisibility_RESOURCE_PUBLIC, grpc.MarshalID(org.ID))

	ratingClient := pb.NewRatingServiceClient(suite.grpcClient)
	_, err = ratingClient.RateRepo(ctx, &pb.RateRepoRequest{
		RepoId:   pbRepo1.Id,
		Reaction: pb.RatingReactionType_RR_TYPE_POSITIVE_LEVEL_20,
	})
	require.NoError(t, err)

	repoClient := pb.NewRepoServiceClient(suite.grpcClient)
	repo1, err := repoClient.Get(ctx, &pb.GetRepositoryRequest{
		Repo: &pb.GetRepositoryRequest_Id{
			Id: pbRepo1.Id,
		},
	})
	require.NoError(t, err)
	repo2, err := repoClient.Get(ctx, &pb.GetRepositoryRequest{
		Repo: &pb.GetRepositoryRequest_Id{
			Id: pbRepo2.Id,
		},
	})
	require.NoError(t, err)

	require.NotNil(t, repo1.Rating)
	require.EqualValues(t, 100.0, repo1.Rating.Percentile)
	require.NotNil(t, repo2.Rating)
	require.EqualValues(t, 100.0, repo2.Rating.Percentile)
}

func (suite *RwApiTestSuite) TestGrpcRepoReactions_RepoReactionsCount() {
	t := suite.T()
	user := suite.users.Kopatych
	org := suite.orgs.Yandex
	suite.addOrgRole(t, user, org, iam.Roles.RepositoriesDeveloper)

	ratingClient := pb.NewRatingServiceClient(suite.grpcClient)
	repoClient := pb.NewRepoServiceClient(suite.grpcClient)

	pbRepo, _ := suite.createRepo(t, user, "repo", pb.ResourceVisibility_RESOURCE_PUBLIC, grpc.MarshalID(org.ID))

	for _, r := range []struct {
		user     *entities.User
		reaction pb.RatingReactionType
	}{
		{
			user:     suite.users.Kopatych,
			reaction: pb.RatingReactionType_RR_TYPE_POSITIVE_LEVEL_10,
		},
		{
			user:     suite.users.Krosh,
			reaction: pb.RatingReactionType_RR_TYPE_POSITIVE_LEVEL_30,
		},
		{
			user:     suite.users.Barash,
			reaction: pb.RatingReactionType_RR_TYPE_POSITIVE_LEVEL_20,
		},
		{
			user:     suite.users.PinPublic,
			reaction: pb.RatingReactionType_RR_TYPE_POSITIVE_LEVEL_20,
		},
		{
			user:     suite.users.BiBiPublic,
			reaction: pb.RatingReactionType_RR_TYPE_POSITIVE_LEVEL_20,
		},
		{
			user:     suite.users.Pikachu,
			reaction: pb.RatingReactionType_RR_TYPE_POSITIVE_LEVEL_30,
		},
	} {
		_, err := ratingClient.RateRepo(testutils.AuthorizeGRPC(r.user.Identity), &pb.RateRepoRequest{
			RepoId:   pbRepo.Id,
			Reaction: r.reaction,
		})
		require.NoError(t, err)
	}

	repo, err := repoClient.Get(testutils.AuthorizeGRPC(user.Identity), &pb.GetRepositoryRequest{
		Repo: &pb.GetRepositoryRequest_Id{
			Id: pbRepo.Id,
		},
	})
	require.NoError(t, err)
	//yarequire.ProtoDumpFixture(t, repo.Rating)
	yarequire.ProtoCompareWithFixture(t, repo.Rating)
}

func (suite *RwApiTestSuite) TestGrpcRepoReactions_ListCheckAccess() {
	t := suite.T()
	user := suite.users.Kopatych
	org := suite.orgs.Yandex
	ctx := testutils.AuthorizeGRPC(user.Identity)

	pbRepo1, _ := suite.createRepo(t, suite.users.Admin, "test-repo-1", pb.ResourceVisibility_RESOURCE_PUBLIC, grpc.MarshalID(org.ID))
	pbRepo2, _ := suite.createRepo(t, suite.users.Admin, "test-repo-2", pb.ResourceVisibility_RESOURCE_PRIVATE, grpc.MarshalID(org.ID))
	pbRepo3, _ := suite.createRepo(t, suite.users.Admin, "test-repo-3", pb.ResourceVisibility_RESOURCE_PRIVATE, grpc.MarshalID(org.ID))

	getID := func(repo *pb.Repository) uint64 {
		id, err := grpc.ParseID(repo.Id)
		require.NoError(t, err)
		return id
	}

	// set as "unfinished migration"
	err := suite.RepoRepo.UpdateRepositoryByID(getID(pbRepo3)).
		SetMigrationID(utils.PtrFromValue("not-nil")).
		SetIsMigrating(true).
		Commit(ctx)
	require.NoError(t, err)

	require.NoError(t, suite.RatingReactionsRepo.Upsert(ctx, &entities.RatingReaction{
		UserID:     user.ID,
		EntityID:   getID(pbRepo1),
		EntityType: entities.EntityTypes.Repository,
		Reaction:   entities.RatingReactionTypes.PositiveLevel10,
	}))
	require.NoError(t, suite.RatingReactionsRepo.Upsert(ctx, &entities.RatingReaction{
		UserID:     user.ID,
		EntityID:   getID(pbRepo2),
		EntityType: entities.EntityTypes.Repository,
		Reaction:   entities.RatingReactionTypes.PositiveLevel20,
	}))
	require.NoError(t, suite.RatingReactionsRepo.Upsert(ctx, &entities.RatingReaction{
		UserID:     user.ID,
		EntityID:   getID(pbRepo3),
		EntityType: entities.EntityTypes.Repository,
		Reaction:   entities.RatingReactionTypes.PositiveLevel30,
	}))

	ratingClient := pb.NewRatingServiceClient(suite.grpcClient)

	check := func(expectIDs []string) {
		response, err := ratingClient.ListMyReactions(ctx, &pb.ListMyRatingReactionsRequest{})
		require.NoError(t, err)

		gotIDs := functools.Map(response.Reactions, (*pb.MyRatingReaction).GetRepoId)
		slices.Sort(expectIDs)
		slices.Sort(gotIDs)
		require.Equal(t, expectIDs, gotIDs)
	}

	check([]string{pbRepo1.Id})
	suite.addOrgRole(t, user, org, iam.Roles.RepositoriesDeveloper)
	check([]string{pbRepo1.Id, pbRepo2.Id})
	suite.addOrgRole(t, user, org, iam.Roles.Admin)
	check([]string{pbRepo1.Id, pbRepo2.Id, pbRepo3.Id})
}

func (suite *RwApiTestSuite) TestGrpcRepoReactions_ListMyReactions_DeletedRepo() {
	t := suite.T()
	user := suite.users.Kopatych
	org := suite.orgs.Yandex
	ctx := testutils.AuthorizeGRPC(user.Identity)
	suite.addOrgRole(t, user, org, iam.Roles.Admin)

	ratingClient := pb.NewRatingServiceClient(suite.grpcClient)

	pbRepo1, _ := suite.createRepo(t, user, "test-repo-kept", pb.ResourceVisibility_RESOURCE_PUBLIC, grpc.MarshalID(org.ID))
	pbRepo2, _ := suite.createRepo(t, user, "test-repo-deleted", pb.ResourceVisibility_RESOURCE_PUBLIC, grpc.MarshalID(org.ID))

	getID := func(repo *pb.Repository) uint64 {
		id, err := grpc.ParseID(repo.Id)
		require.NoError(t, err)
		return id
	}

	// Add reactions to both repos
	_, err := ratingClient.RateRepo(ctx, &pb.RateRepoRequest{
		RepoId:   pbRepo1.Id,
		Reaction: pb.RatingReactionType_RR_TYPE_POSITIVE_LEVEL_20,
	})
	require.NoError(t, err)

	_, err = ratingClient.RateRepo(ctx, &pb.RateRepoRequest{
		RepoId:   pbRepo2.Id,
		Reaction: pb.RatingReactionType_RR_TYPE_POSITIVE_LEVEL_30,
	})
	require.NoError(t, err)

	// Verify both reactions are returned
	response, err := ratingClient.ListMyReactions(ctx, &pb.ListMyRatingReactionsRequest{})
	require.NoError(t, err)
	require.Len(t, response.Reactions, 2)

	// Delete the second repo
	require.NoError(t, suite.RepoService.DeleteRepository(ctx, getID(pbRepo2), user.ID))

	// ListMyReactions should still work and only return the first repo
	response, err = ratingClient.ListMyReactions(ctx, &pb.ListMyRatingReactionsRequest{})
	require.NoError(t, err)
	require.Len(t, response.Reactions, 1)
	require.Equal(t, pbRepo1.Id, response.Reactions[0].RepoId)
}

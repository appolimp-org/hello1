package integrationtests

import (
	"common/testutils/yarequire"
	"common/utils"
	"context"
	"gitcore/internal/entities"
	"gitcore/internal/entities/notifications"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/testutils"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/testing/protocmp"
)

func (suite *RwApiTestSuite) TestGrpcAchievementNotifications() {
	t := suite.T()
	client := pb.NewAchievementServiceClient(suite.grpcClient)

	// user from whitelist
	robot := suite.UserFixture(testutils.UserIdentities.Robot, entities.Visibilities.Private)
	robotCtx := testutils.AuthorizeGRPC(robot.Identity)

	t.Run("UnlockAchievement", func(t *testing.T) {
		// unlock the first achievement, must recieve a notification

		_, err := client.UnlockAchievement(robotCtx, &pb.UnlockAchievementRequest{
			UserId: grpc_marshalling.IDInverse(suite.users.Krosh.ID),
			Achievement: &pb.UnlockAchievementRequest_Id{
				Id: grpc_marshalling.IDInverse(suite.achievements.Variants.ID),
			},
			Seed:  utils.PtrFromValue((uint64)(777)),
			Level: 0,
		})
		require.NoError(t, err)

		suite.WaitForWorkflows(t, entities.WorkflowTypes.SendNotifications)
		notifyHistory := suite.NotifyMessages(context.Background())
		require.Len(t, notifyHistory, 1)
		data, ok := notifyHistory[0].Data.(*notifications.AchievementUnlockedParams)
		require.True(t, ok)
		require.Equal(t, "/nice-achievement-3.png", data.Achievement.PictureURL)
		require.Equal(t, uint64(1), data.Achievement.Level)
		require.Equal(t, "variants-achievement", data.Achievement.Slug)
		require.Equal(t, suite.users.Krosh.Username, data.User.Login)
		suite.ClearNotifyMessages()

		// repeated unlock, must NOT recieve a notification

		_, err = client.UnlockAchievement(robotCtx, &pb.UnlockAchievementRequest{
			UserId: grpc_marshalling.IDInverse(suite.users.Krosh.ID),
			Achievement: &pb.UnlockAchievementRequest_Id{
				Id: grpc_marshalling.IDInverse(suite.achievements.Variants.ID),
			},
			Level: 0,
		})
		require.NoError(t, err)

		suite.WaitForWorkflows(t, entities.WorkflowTypes.SendNotifications)
		notifyHistory = suite.NotifyMessages(context.Background())
		require.Len(t, notifyHistory, 0)
		suite.ClearNotifyMessages()

		// unlock with silent mode

		_, err = client.UnlockAchievement(robotCtx, &pb.UnlockAchievementRequest{
			UserId: grpc_marshalling.IDInverse(suite.users.Krosh.ID),
			Achievement: &pb.UnlockAchievementRequest_Id{
				Id: grpc_marshalling.IDInverse(suite.achievements.Rare.ID),
			},
			Seed:   utils.PtrFromValue((uint64)(0)),
			Level:  0,
			Silent: true,
		})
		require.NoError(t, err)

		suite.WaitForWorkflows(t, entities.WorkflowTypes.SendNotifications)
		notifyHistory = suite.NotifyMessages(context.Background())
		require.Len(t, notifyHistory, 0)
		suite.ClearNotifyMessages()
	})

}

func (suite *RwApiTestSuite) TestGrpcAchievements() {
	t := suite.T()
	client := pb.NewAchievementServiceClient(suite.grpcClient)

	// public user
	kopatychCtx := testutils.AuthorizeGRPC(suite.users.Kopatych.Identity)
	// user from whitelist
	robot := suite.UserFixture(testutils.UserIdentities.Robot, entities.Visibilities.Private)
	robotCtx := testutils.AuthorizeGRPC(robot.Identity)

	anonymousCtx := context.Background()

	t.Run("AnonymousAccess", func(t *testing.T) {
		_, err := client.ListPublicAchievements(anonymousCtx, &pb.ListPublicAchievementsRequest{})
		require.NoError(t, err)

		_, err = client.GetAchievement(anonymousCtx, &pb.GetAchievementRequest{
			AchievementSlug: &suite.achievements.First.Slug,
		})
		require.NoError(t, err)

		_, err = client.ListAchievementUnlockers(anonymousCtx, &pb.ListAchievementUnlockersRequest{
			Achievement: &pb.ListAchievementUnlockersRequest_Id{
				Id: grpc_marshalling.IDInverse(suite.achievements.First.ID),
			},
		})
		require.NoError(t, err)
	})

	t.Run("ListPublicAchievements", func(t *testing.T) {
		// list public achievements
		respPublic, err := client.ListPublicAchievements(kopatychCtx, &pb.ListPublicAchievementsRequest{})
		require.NoError(t, err)
		// yarequire.ProtoDumpFixture(t, respPublic)
		yarequire.ProtoCompareWithFixture(t, respPublic,
			protocmp.IgnoreFields(&pb.Achievement{}, "created_at"))

		// filter achievements by category
		categoryCode := pb.AchievementCategory_CATEGORY_CODE
		respCode, err := client.ListPublicAchievements(kopatychCtx, &pb.ListPublicAchievementsRequest{
			Category: &categoryCode,
		})
		require.NoError(t, err)
		require.Equal(t, len(respCode.Achievements), 1)
		require.Equal(t, respCode.Achievements[0].Slug, "first-achievement")

		categoryCommunity := pb.AchievementCategory_CATEGORY_COMMUNITY
		respCommunity, err := client.ListPublicAchievements(kopatychCtx, &pb.ListPublicAchievementsRequest{
			Category: &categoryCommunity,
		})
		require.NoError(t, err)
		require.Equal(t, len(respCommunity.Achievements), 1)
		require.Equal(t, respCommunity.Achievements[0].Slug, "levellable-achievement")

	})

	t.Run("GetAchievement", func(t *testing.T) {
		// get the first achievement by id
		respID, err := client.GetAchievement(kopatychCtx, &pb.GetAchievementRequest{
			AchievementId: grpc_marshalling.IDNullableInverse(suite.achievements.First.ID),
		})
		require.NoError(t, err)

		// get the first achievement by slug
		respSlug, err := client.GetAchievement(kopatychCtx, &pb.GetAchievementRequest{
			AchievementSlug: &suite.achievements.First.Slug,
		})
		require.NoError(t, err)

		yarequire.ProtoEqual(t, respID, respSlug)
		// yarequire.ProtoDumpFixture(t, respID)
		yarequire.ProtoCompareWithFixture(t, respID,
			protocmp.IgnoreFields(&pb.Achievement{}, "created_at"))
	})

	t.Run("UnlockAchievement", func(t *testing.T) {
		// unlock the first achievement
		_, err := client.UnlockAchievement(robotCtx, &pb.UnlockAchievementRequest{
			UserId: grpc_marshalling.IDInverse(suite.users.Krosh.ID),
			Achievement: &pb.UnlockAchievementRequest_Id{
				Id: grpc_marshalling.IDInverse(suite.achievements.First.ID),
			},
			Seed:  utils.PtrFromValue((uint64)(0)),
			Level: 0,
		})
		require.NoError(t, err)

		// unlock the second achievement
		_, err = client.UnlockAchievement(robotCtx, &pb.UnlockAchievementRequest{
			UserId: grpc_marshalling.IDInverse(suite.users.Krosh.ID),
			Achievement: &pb.UnlockAchievementRequest_Slug{
				Slug: suite.achievements.Rare.Slug,
			},
			Level: 2,
			Seed:  utils.PtrFromValue((uint64)(0)),
		})
		require.NoError(t, err)

		// repeated unlock
		_, err = client.UnlockAchievement(robotCtx, &pb.UnlockAchievementRequest{
			UserId: grpc_marshalling.IDInverse(suite.users.Krosh.ID),
			Achievement: &pb.UnlockAchievementRequest_Slug{
				Slug: suite.achievements.Rare.Slug,
			},
			Level: 2,
			Seed:  utils.PtrFromValue((uint64)(0)),
		})
		require.NoError(t, err)

		// check two unlocked achievements at db
		respUnlocked, err := client.ListUnlockedAchievements(kopatychCtx, &pb.ListUnlockedAchievementsRequest{
			UserId: grpc_marshalling.IDInverse(suite.users.Krosh.ID),
		})
		require.NoError(t, err)
		//yarequire.ProtoDumpFixture(t, respUnlocked,
		//	protocmp.IgnoreFields(&pb.UnlockedAchievement{}, "unlocked_at"))
		yarequire.ProtoCompareWithFixture(t, respUnlocked,
			protocmp.IgnoreFields(&pb.UnlockedAchievement{}, "unlocked_at"),
			protocmp.IgnoreFields(&pb.Achievement{}, "created_at"),
			protocmp.IgnoreFields(&pb.ListUnlockedAchievementsResponse{}, "revision"),
		)
	})

	t.Run("ListAchievementUnlockers", func(t *testing.T) {
		// unlock the first achievement for Slowpoke
		_, err := client.UnlockAchievement(robotCtx, &pb.UnlockAchievementRequest{
			UserId: grpc_marshalling.IDInverse(suite.users.Slowpoke.ID),
			Achievement: &pb.UnlockAchievementRequest_Id{
				Id: grpc_marshalling.IDInverse(suite.achievements.First.ID),
			},
			Seed:  utils.PtrFromValue((uint64)(0)),
			Level: 2,
		})
		require.NoError(t, err)

		// get unlockers of the first achievement by id
		respID, err := client.ListAchievementUnlockers(kopatychCtx, &pb.ListAchievementUnlockersRequest{
			Achievement: &pb.ListAchievementUnlockersRequest_Id{
				Id: grpc_marshalling.IDInverse(suite.achievements.First.ID),
			},
		})
		require.NoError(t, err)

		// get unlockers of the first achievement by slug
		respSlug, err := client.ListAchievementUnlockers(kopatychCtx, &pb.ListAchievementUnlockersRequest{
			Achievement: &pb.ListAchievementUnlockersRequest_Slug{
				Slug: suite.achievements.First.Slug,
			},
		})
		require.NoError(t, err)

		yarequire.ProtoEqual(t, respID, respSlug)
		// yarequire.ProtoDumpFixture(t, respID)
		yarequire.ProtoCompareWithFixture(t, respID,
			protocmp.IgnoreFields(&pb.Achievement{}, "created_at"))

		// remove the first achievement of Slowpoke
		_, err = client.LockAchievement(robotCtx, &pb.LockAchievementRequest{
			UserId: grpc_marshalling.IDInverse(suite.users.Slowpoke.ID),
			Achievement: &pb.LockAchievementRequest_Id{
				Id: grpc_marshalling.IDInverse(suite.achievements.First.ID),
			},
		})
		require.NoError(t, err)
	})

	t.Run("LockAchievement", func(t *testing.T) {
		// remove the first achievement
		_, err := client.LockAchievement(robotCtx, &pb.LockAchievementRequest{
			UserId: grpc_marshalling.IDInverse(suite.users.Krosh.ID),
			Achievement: &pb.LockAchievementRequest_Id{
				Id: grpc_marshalling.IDInverse(suite.achievements.First.ID),
			},
		})
		require.NoError(t, err)

		// check an unlocked achievement at db
		respUnlocked, err := client.ListUnlockedAchievements(kopatychCtx, &pb.ListUnlockedAchievementsRequest{
			UserId: grpc_marshalling.IDInverse(suite.users.Krosh.ID),
		})
		require.NoError(t, err)
		// yarequire.ProtoDumpFixture(t, respUnlocked,
		// 	protocmp.IgnoreFields(&pb.UnlockedAchievement{}, "unlocked_at"))
		yarequire.ProtoCompareWithFixture(t, respUnlocked,
			protocmp.IgnoreFields(&pb.UnlockedAchievement{}, "unlocked_at"),
			protocmp.IgnoreFields(&pb.Achievement{}, "created_at"),
			protocmp.IgnoreFields(&pb.ListUnlockedAchievementsResponse{}, "revision"))

		// remove the second achievement
		_, err = client.LockAchievement(robotCtx, &pb.LockAchievementRequest{
			UserId: grpc_marshalling.IDInverse(suite.users.Krosh.ID),
			Achievement: &pb.LockAchievementRequest_Slug{
				Slug: suite.achievements.Rare.Slug,
			},
		})
		require.NoError(t, err)

		// repeated lock
		_, err = client.LockAchievement(robotCtx, &pb.LockAchievementRequest{
			UserId: grpc_marshalling.IDInverse(suite.users.Krosh.ID),
			Achievement: &pb.LockAchievementRequest_Slug{
				Slug: suite.achievements.Rare.Slug,
			},
		})
		require.NoError(t, err)

		// check empty db
		respUnlocked, err = client.ListUnlockedAchievements(kopatychCtx, &pb.ListUnlockedAchievementsRequest{
			UserId: grpc_marshalling.IDInverse(suite.users.Krosh.ID),
		})
		require.NoError(t, err)
		require.Equal(t, len(respUnlocked.Achievements), 0)
	})

	t.Run("UnlockWithSeed", func(t *testing.T) {
		// unlock the first achievement
		_, err := client.UnlockAchievement(robotCtx, &pb.UnlockAchievementRequest{
			UserId: grpc_marshalling.IDInverse(suite.users.Krosh.ID),
			Achievement: &pb.UnlockAchievementRequest_Id{
				Id: grpc_marshalling.IDInverse(suite.achievements.Variants.ID),
			},
			Level: 0,
			Seed:  utils.PtrFromValue(uint64(3)),
		})
		require.NoError(t, err)

		// check two unlocked achievements at db
		respUnlocked, err := client.ListUnlockedAchievements(kopatychCtx, &pb.ListUnlockedAchievementsRequest{
			UserId: grpc_marshalling.IDInverse(suite.users.Krosh.ID),
		})
		require.NoError(t, err)

		//yarequire.ProtoDumpFixture(t, respUnlocked)

		yarequire.ProtoCompareWithFixture(t, respUnlocked,
			protocmp.IgnoreFields(&pb.UnlockedAchievement{}, "unlocked_at"),
			protocmp.IgnoreFields(&pb.Achievement{}, "created_at"),
			protocmp.IgnoreFields(&pb.ListUnlockedAchievementsResponse{}, "revision"),
		)
	})
}

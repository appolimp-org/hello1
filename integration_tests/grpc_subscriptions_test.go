package integrationtests

import (
	"common/grpc"
	"common/testutils/yarequire"
	"common/utils"
	"common/utils/rolesgenerator/iam"
	"context"
	"gitcore/internal/entities"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/testutils"
	pagination_pb "private_api/generated/yandex/cloud/pagination"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/testing/protocmp"
)

func (suite *RwApiTestSuite) TestSubscriptions_Get() {
	c := pb.NewSubscriptionServiceClient(suite.grpcClient)

	// create
	op, err := c.UpdateSubscriptionType(testutils.AuthorizeGRPC(testutils.UserIdentities.Kopatych), &pb.UpdateSubscriptionTypeRequest{
		Object: &pb.ObjectIdentity{
			Type: pb.ObjectIdentity_ORGANIZATION,
			Id:   grpc.MarshalID(suite.orgs.Yandex.ID),
		},
		Type: pb.Subscription_TYPE_NOTIFY,
	})
	require.NoError(suite.T(), err)

	subscription := testutils.UnmarshalGrpcResult[*pb.Subscription](suite.T(), op)

	type args struct {
		ctx     context.Context
		request *pb.GetSubscriptionRequest
	}
	tests := []*struct {
		name        string
		args        args
		want        *pb.GetSubscriptionResponse
		wantErrCode codes.Code
	}{
		{
			name: "ok",
			args: args{
				ctx: testutils.AuthorizeGRPC(testutils.UserIdentities.Kopatych),
				request: &pb.GetSubscriptionRequest{
					Object: &pb.ObjectIdentity{
						Type: pb.ObjectIdentity_ORGANIZATION,
						Id:   grpc.MarshalID(suite.orgs.Yandex.ID),
					},
				},
			},
			want: &pb.GetSubscriptionResponse{
				Subscription: subscription,
			},
		},
		{
			name: "invalid id",
			args: args{
				ctx: testutils.AuthorizeGRPC(testutils.UserIdentities.Krosh),
				request: &pb.GetSubscriptionRequest{
					Object: &pb.ObjectIdentity{
						Type: pb.ObjectIdentity_ORGANIZATION,
						Id:   "invalid_id",
					},
				},
			},
			wantErrCode: codes.InvalidArgument,
		},
		{
			name: "not found",
			args: args{
				ctx: testutils.AuthorizeGRPC(testutils.UserIdentities.Krosh),
				request: &pb.GetSubscriptionRequest{
					Object: &pb.ObjectIdentity{
						Type: pb.ObjectIdentity_ORGANIZATION,
						Id:   subscription.Object.Id,
					},
				},
			},
			want: &pb.GetSubscriptionResponse{
				Subscription: &pb.Subscription{
					Type: pb.Subscription_TYPE_DEFAULT,
					Object: &pb.ObjectIdentity{
						Type: pb.ObjectIdentity_ORGANIZATION,
						Id:   grpc.MarshalID(suite.orgs.Yandex.ID),
					},
				},
			},
		},
	}

	for _, tt := range tests {
		suite.T().Run(tt.name, func(t *testing.T) {
			resp, err := c.Get(tt.args.ctx, tt.args.request)

			if tt.wantErrCode != 0 {
				st, ok := status.FromError(err)
				require.True(t, ok)
				require.Equal(t, tt.wantErrCode, st.Code())
				return
			}
			require.NoError(t, err)

			// fill changed fields
			if resp.Subscription.Type != pb.Subscription_TYPE_DEFAULT {
				require.NotEmpty(t, resp.Subscription.CreatedAt)
			}
			tt.want.Subscription.CreatedAt = resp.Subscription.CreatedAt

			yarequire.ProtoCmp(t, tt.want, resp,
				protocmp.IgnoreFields(&pb.GetSubscriptionResponse{}, "notification"),
				protocmp.IgnoreFields(&pb.Subscription{}, "created_at"),
			)
		})
	}
}

func (suite *RwApiTestSuite) TestSubscriptions_GetNotification() {
	c := pb.NewSubscriptionServiceClient(suite.grpcClient)

	repo := suite.repos.Alpha
	pr := suite.makePullRequest(suite.users.Kopatych, nil)

	type args struct {
		repoSubType *entities.SubscriptionType
		prSupType   *entities.SubscriptionType
	}
	type testcase struct {
		name string
		user *entities.User
		args args
		want *pb.GetSubscriptionResponse
	}

	tests := []testcase{
		{
			name: "mute because no sub",
			user: suite.users.Kopatych,
			args: args{},
			want: &pb.GetSubscriptionResponse{
				Subscription: &pb.Subscription{
					Type: pb.Subscription_TYPE_DEFAULT,
					Object: &pb.ObjectIdentity{
						Type: pb.ObjectIdentity_PULL_REQUEST,
						Id:   grpc.MarshalID(pr.ID),
					},
				},
				Notification: &pb.GetSubscriptionResponse_Notification{
					WillReceive: false,
					Reason: &pb.Subscription{
						Type: pb.Subscription_TYPE_DEFAULT,
						Object: &pb.ObjectIdentity{
							Type: pb.ObjectIdentity_PULL_REQUEST,
							Id:   grpc.MarshalID(pr.ID),
						},
					},
				},
			},
		},
		{
			name: "notify because object",
			user: suite.users.Krosh,
			args: args{
				prSupType: &entities.SubscriptionTypes.Notify,
			},
			want: &pb.GetSubscriptionResponse{
				Subscription: &pb.Subscription{
					Type: pb.Subscription_TYPE_NOTIFY,
					Object: &pb.ObjectIdentity{
						Type: pb.ObjectIdentity_PULL_REQUEST,
						Id:   grpc.MarshalID(pr.ID),
					},
				},
				Notification: &pb.GetSubscriptionResponse_Notification{
					WillReceive: true,
					Reason: &pb.Subscription{
						Type: pb.Subscription_TYPE_NOTIFY,
						Object: &pb.ObjectIdentity{
							Type: pb.ObjectIdentity_PULL_REQUEST,
							Id:   grpc.MarshalID(pr.ID),
						},
					},
				},
			},
		},
		{
			name: "notify because parent",
			user: suite.users.Barash,
			args: args{
				repoSubType: &entities.SubscriptionTypes.Notify,
			},
			want: &pb.GetSubscriptionResponse{
				Subscription: &pb.Subscription{
					Type: pb.Subscription_TYPE_DEFAULT,
					Object: &pb.ObjectIdentity{
						Type: pb.ObjectIdentity_PULL_REQUEST,
						Id:   grpc.MarshalID(pr.ID),
					},
				},
				Notification: &pb.GetSubscriptionResponse_Notification{
					WillReceive: true,
					Reason: &pb.Subscription{
						Type: pb.Subscription_TYPE_NOTIFY,
						Object: &pb.ObjectIdentity{
							Type: pb.ObjectIdentity_REPOSITORY,
							Id:   grpc.MarshalID(repo.ID),
						},
					},
				},
			},
		},
		{
			// TODO may be changed if behavior in SubscriptionService.GetPrioritySubscription changes
			name: "mute because parent",
			user: suite.users.Pikachu,
			args: args{
				repoSubType: &entities.SubscriptionTypes.Mute,
				prSupType:   &entities.SubscriptionTypes.Notify,
			},
			want: &pb.GetSubscriptionResponse{
				Subscription: &pb.Subscription{
					Type: pb.Subscription_TYPE_NOTIFY,
					Object: &pb.ObjectIdentity{
						Type: pb.ObjectIdentity_PULL_REQUEST,
						Id:   grpc.MarshalID(pr.ID),
					},
				},
				Notification: &pb.GetSubscriptionResponse_Notification{
					WillReceive: false,
					Reason: &pb.Subscription{
						Type: pb.Subscription_TYPE_MUTE,
						Object: &pb.ObjectIdentity{
							Type: pb.ObjectIdentity_REPOSITORY,
							Id:   grpc.MarshalID(repo.ID),
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		suite.T().Run(tt.name, func(t *testing.T) {
			if tt.args.repoSubType != nil {
				op, err := c.UpdateSubscriptionType(testutils.AuthorizeGRPC(tt.user.Identity), &pb.UpdateSubscriptionTypeRequest{
					Object: &pb.ObjectIdentity{
						Type: pb.ObjectIdentity_REPOSITORY,
						Id:   grpc.MarshalID(repo.ID),
					},
					Type: grpc_marshalling.SubscriptionTypeInverse(*tt.args.repoSubType),
				})
				require.NoError(t, err)
				require.True(t, op.Done)
			}

			if tt.args.prSupType != nil {
				op, err := c.UpdateSubscriptionType(testutils.AuthorizeGRPC(tt.user.Identity), &pb.UpdateSubscriptionTypeRequest{
					Object: &pb.ObjectIdentity{
						Type: pb.ObjectIdentity_PULL_REQUEST,
						Id:   grpc.MarshalID(pr.ID),
					},
					Type: grpc_marshalling.SubscriptionTypeInverse(*tt.args.prSupType),
				})
				require.NoError(t, err)
				require.True(t, op.Done)
			}

			response, err := c.Get(
				testutils.AuthorizeGRPC(tt.user.Identity),
				&pb.GetSubscriptionRequest{
					Object: &pb.ObjectIdentity{
						Type: pb.ObjectIdentity_PULL_REQUEST,
						Id:   grpc.MarshalID(pr.ID),
					},
				},
			)
			require.NoError(t, err)

			yarequire.ProtoCmp(t, tt.want, response,
				protocmp.IgnoreFields(&pb.Subscription{}, "created_at", "reason"),
			)

		})
	}
}

func (suite *RwApiTestSuite) TestSubscriptions_GetIssueNotification() {
	c := pb.NewSubscriptionServiceClient(suite.grpcClient)

	repo := suite.repos.Alpha
	issue := suite.makeIssue(suite.users.Kopatych, &makeIssueOptions{
		RepoID:     repo.ID,
		Title:      "SomeIssue",
		Visibility: entities.IssueVisibilities.Public,
	})

	type args struct {
		repoSubType  *entities.SubscriptionType
		issueSubType *entities.SubscriptionType
	}
	type testcase struct {
		name string
		user *entities.User
		args args
		want *pb.GetSubscriptionResponse
	}

	tests := []testcase{
		{
			name: "mute because no sub",
			user: suite.users.Kopatych,
			args: args{},
			want: &pb.GetSubscriptionResponse{
				Subscription: &pb.Subscription{
					Type: pb.Subscription_TYPE_DEFAULT,
					Object: &pb.ObjectIdentity{
						Type: pb.ObjectIdentity_ISSUE,
						Id:   grpc.MarshalID(issue.ID),
					},
				},
				Notification: &pb.GetSubscriptionResponse_Notification{
					WillReceive: false,
					Reason: &pb.Subscription{
						Type: pb.Subscription_TYPE_DEFAULT,
						Object: &pb.ObjectIdentity{
							Type: pb.ObjectIdentity_ISSUE,
							Id:   grpc.MarshalID(issue.ID),
						},
					},
				},
			},
		},
		{
			name: "notify because object",
			user: suite.users.Krosh,
			args: args{
				issueSubType: &entities.SubscriptionTypes.Notify,
			},
			want: &pb.GetSubscriptionResponse{
				Subscription: &pb.Subscription{
					Type: pb.Subscription_TYPE_NOTIFY,
					Object: &pb.ObjectIdentity{
						Type: pb.ObjectIdentity_ISSUE,
						Id:   grpc.MarshalID(issue.ID),
					},
				},
				Notification: &pb.GetSubscriptionResponse_Notification{
					WillReceive: true,
					Reason: &pb.Subscription{
						Type: pb.Subscription_TYPE_NOTIFY,
						Object: &pb.ObjectIdentity{
							Type: pb.ObjectIdentity_ISSUE,
							Id:   grpc.MarshalID(issue.ID),
						},
					},
				},
			},
		},
		{
			name: "notify because parent",
			user: suite.users.Barash,
			args: args{
				repoSubType: &entities.SubscriptionTypes.Notify,
			},
			want: &pb.GetSubscriptionResponse{
				Subscription: &pb.Subscription{
					Type: pb.Subscription_TYPE_DEFAULT,
					Object: &pb.ObjectIdentity{
						Type: pb.ObjectIdentity_ISSUE,
						Id:   grpc.MarshalID(issue.ID),
					},
				},
				Notification: &pb.GetSubscriptionResponse_Notification{
					WillReceive: true,
					Reason: &pb.Subscription{
						Type: pb.Subscription_TYPE_NOTIFY,
						Object: &pb.ObjectIdentity{
							Type: pb.ObjectIdentity_REPOSITORY,
							Id:   grpc.MarshalID(repo.ID),
						},
					},
				},
			},
		},
		{
			name: "mute because parent",
			user: suite.users.Pikachu,
			args: args{
				repoSubType:  &entities.SubscriptionTypes.Mute,
				issueSubType: &entities.SubscriptionTypes.Notify,
			},
			want: &pb.GetSubscriptionResponse{
				Subscription: &pb.Subscription{
					Type: pb.Subscription_TYPE_NOTIFY,
					Object: &pb.ObjectIdentity{
						Type: pb.ObjectIdentity_ISSUE,
						Id:   grpc.MarshalID(issue.ID),
					},
				},
				Notification: &pb.GetSubscriptionResponse_Notification{
					WillReceive: false,
					Reason: &pb.Subscription{
						Type: pb.Subscription_TYPE_MUTE,
						Object: &pb.ObjectIdentity{
							Type: pb.ObjectIdentity_REPOSITORY,
							Id:   grpc.MarshalID(repo.ID),
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		suite.T().Run(tt.name, func(t *testing.T) {
			if tt.args.repoSubType != nil {
				op, err := c.UpdateSubscriptionType(testutils.AuthorizeGRPC(tt.user.Identity), &pb.UpdateSubscriptionTypeRequest{
					Object: &pb.ObjectIdentity{
						Type: pb.ObjectIdentity_REPOSITORY,
						Id:   grpc.MarshalID(repo.ID),
					},
					Type: grpc_marshalling.SubscriptionTypeInverse(*tt.args.repoSubType),
				})
				require.NoError(t, err)
				require.True(t, op.Done)
			}

			if tt.args.issueSubType != nil {
				op, err := c.UpdateSubscriptionType(testutils.AuthorizeGRPC(tt.user.Identity), &pb.UpdateSubscriptionTypeRequest{
					Object: &pb.ObjectIdentity{
						Type: pb.ObjectIdentity_ISSUE,
						Id:   grpc.MarshalID(issue.ID),
					},
					Type: grpc_marshalling.SubscriptionTypeInverse(*tt.args.issueSubType),
				})
				require.NoError(t, err)
				require.True(t, op.Done)
			}

			response, err := c.Get(
				testutils.AuthorizeGRPC(tt.user.Identity),
				&pb.GetSubscriptionRequest{
					Object: &pb.ObjectIdentity{
						Type: pb.ObjectIdentity_ISSUE,
						Id:   grpc.MarshalID(issue.ID),
					},
				},
			)
			require.NoError(t, err)

			yarequire.ProtoCmp(t, tt.want, response,
				protocmp.IgnoreFields(&pb.Subscription{}, "created_at", "reason"),
			)
		})
	}
}

func (suite *RwApiTestSuite) TestSubscriptions_Access() {
	t := suite.T()

	c := pb.NewSubscriptionServiceClient(suite.grpcClient)
	repo := suite.repos.History
	user := testutils.UserIdentities.Krosh
	ctx := testutils.AuthorizeGRPC(user)

	pbObject := &pb.ObjectIdentity{
		Type: pb.ObjectIdentity_REPOSITORY,
		Id:   grpc.MarshalID(repo.ID),
	}

	_, err := c.UpdateSubscriptionType(ctx, &pb.UpdateSubscriptionTypeRequest{
		Type:   pb.Subscription_TYPE_NOTIFY,
		Object: pbObject,
	})
	require.NoError(t, err)

	t.Run("has access", func(t *testing.T) {
		sub, err := c.Get(ctx, &pb.GetSubscriptionRequest{
			Object: pbObject,
		})
		require.NoError(t, err)
		require.True(t, sub.Notification.WillReceive)

		_, err = c.UpdateSubscriptionType(ctx, &pb.UpdateSubscriptionTypeRequest{
			Type:   pb.Subscription_TYPE_MUTE,
			Object: pbObject,
		})
		require.NoError(t, err)
	})

	t.Run("no access", func(t *testing.T) {
		err := suite.RepoRepo.UpdateRepositoryByID(repo.ID).
			SetRepoVisibility(entities.Visibilities.Private).
			Commit(ctx)
		require.NoError(t, err)

		_, err = c.Get(ctx, &pb.GetSubscriptionRequest{
			Object: pbObject,
		})
		// Can change it to NotFound, but I didn't find any use case for it
		yarequire.ProtoStatusEqual(t, codes.PermissionDenied, err)

		_, err = c.UpdateSubscriptionType(ctx, &pb.UpdateSubscriptionTypeRequest{
			Type:   pb.Subscription_TYPE_NOTIFY,
			Object: pbObject,
		})
		yarequire.ProtoStatusEqual(t, codes.PermissionDenied, err)
	})
}

func (suite *RwApiTestSuite) TestSubscriptions_Create() {
	c := pb.NewSubscriptionServiceClient(suite.grpcClient)

	defaultRequest := &pb.UpdateSubscriptionTypeRequest{
		Type: pb.Subscription_TYPE_NOTIFY,
		Object: &pb.ObjectIdentity{
			Type: pb.ObjectIdentity_ORGANIZATION,
			Id:   grpc.MarshalID(suite.orgs.Yandex.ID),
		},
	}

	// create
	op, err := c.UpdateSubscriptionType(testutils.AuthorizeGRPC(testutils.UserIdentities.Kopatych), &pb.UpdateSubscriptionTypeRequest{
		Object: &pb.ObjectIdentity{
			Type: pb.ObjectIdentity_ORGANIZATION,
			Id:   grpc.MarshalID(suite.orgs.Yandex.ID),
		},
		Type: pb.Subscription_TYPE_NOTIFY,
	})
	require.NoError(suite.T(), err)
	require.True(suite.T(), op.Done)

	type args struct {
		ctx     context.Context
		request *pb.UpdateSubscriptionTypeRequest
	}
	type testcase struct {
		name             string
		args             args
		wantSubscription *pb.Subscription
		wantErrCode      codes.Code
	}

	tests := []testcase{
		{
			name: "ok",
			args: args{
				ctx:     testutils.AuthorizeGRPC(testutils.UserIdentities.Krosh),
				request: defaultRequest,
			},
			wantSubscription: &pb.Subscription{
				Type: pb.Subscription_TYPE_NOTIFY,
				Object: &pb.ObjectIdentity{
					Type: pb.ObjectIdentity_ORGANIZATION,
					Id:   grpc.MarshalID(suite.orgs.Yandex.ID),
				},
				Reason: pb.Subscription_REASON_MANUAL,
			},
		},
		{
			name: "create already exists",
			args: args{
				ctx:     testutils.AuthorizeGRPC(testutils.UserIdentities.Kopatych),
				request: defaultRequest,
			},
			wantSubscription: &pb.Subscription{
				Type: pb.Subscription_TYPE_NOTIFY,
				Object: &pb.ObjectIdentity{
					Type: pb.ObjectIdentity_ORGANIZATION,
					Id:   grpc.MarshalID(suite.orgs.Yandex.ID),
				},
				Reason: pb.Subscription_REASON_MANUAL,
			},
		},
		{
			name: "object not found",
			args: args{
				ctx: testutils.AuthorizeGRPC(testutils.UserIdentities.Kopatych),
				request: &pb.UpdateSubscriptionTypeRequest{
					Type: pb.Subscription_TYPE_NOTIFY,
					Object: &pb.ObjectIdentity{
						Type: pb.ObjectIdentity_REPOSITORY,
						Id:   "0",
					},
				},
			},
			wantErrCode: codes.NotFound,
		},
	}

	for _, tt := range tests {
		suite.T().Run(tt.name, func(t *testing.T) {
			op, err := c.UpdateSubscriptionType(tt.args.ctx, tt.args.request)
			if tt.wantErrCode != 0 {
				st, ok := status.FromError(err)
				require.True(t, ok)
				require.Equal(t, tt.wantErrCode, st.Code())
				return
			}

			require.NoError(t, err)

			meta, err := op.Metadata.UnmarshalNew()
			require.NoError(t, err)

			yarequire.ProtoEqual(t, &pb.UpdateSubscriptionTypeMetadata{
				Object: tt.args.request.Object,
				Type:   tt.args.request.Type,
			}, meta)

			subscription := testutils.UnmarshalGrpcResult[*pb.Subscription](t, op)
			require.NotEmpty(t, subscription.CreatedAt)
			yarequire.ProtoCmp(t, tt.wantSubscription, subscription, protocmp.IgnoreFields(&pb.Subscription{}, "created_at"))
		})
	}
}

func (suite *RwApiTestSuite) TestSubscriptions_UpdateType() {
	c := pb.NewSubscriptionServiceClient(suite.grpcClient)

	type args struct {
		userIdentity entities.UserIdentity
		request      *pb.UpdateSubscriptionTypeRequest
	}
	type testcase struct {
		name             string
		args             args
		wantSubscription *pb.Subscription
		wantErrCode      codes.Code
	}

	// create
	for _, userIdentity := range []entities.UserIdentity{testutils.UserIdentities.Kopatych, testutils.UserIdentities.Barash} {
		op, err := c.UpdateSubscriptionType(testutils.AuthorizeGRPC(userIdentity), &pb.UpdateSubscriptionTypeRequest{
			Object: &pb.ObjectIdentity{
				Type: pb.ObjectIdentity_ORGANIZATION,
				Id:   grpc.MarshalID(suite.orgs.Yandex.ID),
			},
			Type: pb.Subscription_TYPE_NOTIFY,
		})
		require.NoError(suite.T(), err)
		require.True(suite.T(), op.Done)
	}

	tests := []testcase{
		/*{
			name: "default:ok",
			args: args{
				userIdentity: testutils.UserIdentities.Kopatych,
				request: &pb.UpdateSubscriptionTypeRequest{
					Type: pb.Subscription_TYPE_DEFAULT,
					Object: &pb.ObjectIdentity{
						Type: pb.ObjectIdentity_ORGANIZATION,
						Id:   grpc.MarshalID(suite.orgs.Yandex.ID),
					},
				},
			},
			wantSubscription: &pb.Subscription{
				Type: pb.Subscription_TYPE_DEFAULT,
				Object: &pb.ObjectIdentity{
					Type: pb.ObjectIdentity_ORGANIZATION,
					Id:   grpc.MarshalID(suite.orgs.Yandex.ID),
				},
				Reason: pb.Subscription_REASON_MANUAL,
			},
		},
		{
			name: "default:non existent",
			args: args{
				userIdentity: testutils.UserIdentities.Krosh,
				request: &pb.UpdateSubscriptionTypeRequest{
					Type: pb.Subscription_TYPE_DEFAULT,
					Object: &pb.ObjectIdentity{
						Type: pb.ObjectIdentity_ORGANIZATION,
						Id:   grpc.MarshalID(suite.orgs.Yandex.ID),
					},
				},
			},
			wantSubscription: &pb.Subscription{
				Type: pb.Subscription_TYPE_DEFAULT,
				Object: &pb.ObjectIdentity{
					Type: pb.ObjectIdentity_ORGANIZATION,
					Id:   grpc.MarshalID(suite.orgs.Yandex.ID),
				},
				Reason: pb.Subscription_REASON_MANUAL,
			},
		},*/
		{
			name: "default:object not found",
			args: args{
				userIdentity: testutils.UserIdentities.Kopatych,
				request: &pb.UpdateSubscriptionTypeRequest{
					Type: pb.Subscription_TYPE_DEFAULT,
					Object: &pb.ObjectIdentity{
						Type: pb.ObjectIdentity_ORGANIZATION,
						Id:   "999999",
					},
				},
			},
			wantErrCode: codes.NotFound,
		},
		{
			name: "mute ok",
			args: args{
				userIdentity: testutils.UserIdentities.Barash,
				request: &pb.UpdateSubscriptionTypeRequest{
					Type: pb.Subscription_TYPE_MUTE,
					Object: &pb.ObjectIdentity{
						Type: pb.ObjectIdentity_ORGANIZATION,
						Id:   grpc.MarshalID(suite.orgs.Yandex.ID),
					},
				},
			},
			wantSubscription: &pb.Subscription{
				Type: pb.Subscription_TYPE_MUTE,
				Object: &pb.ObjectIdentity{
					Type: pb.ObjectIdentity_ORGANIZATION,
					Id:   grpc.MarshalID(suite.orgs.Yandex.ID),
				},
				Reason: pb.Subscription_REASON_MANUAL,
			},
		},
	}

	for _, tt := range tests {
		suite.T().Run(tt.name, func(t *testing.T) {
			op, err := c.UpdateSubscriptionType(
				testutils.AuthorizeGRPC(tt.args.userIdentity),
				tt.args.request,
			)
			if tt.wantErrCode != 0 {
				st, ok := status.FromError(err)
				require.True(t, ok)
				require.Equal(t, tt.wantErrCode, st.Code())
				return
			}

			require.NoError(t, err)

			meta, err := op.Metadata.UnmarshalNew()
			require.NoError(t, err)

			yarequire.ProtoEqual(t, &pb.UpdateSubscriptionTypeMetadata{
				Object: tt.args.request.Object,
				Type:   tt.args.request.Type,
			}, meta)

			subscription := testutils.UnmarshalGrpcResult[*pb.Subscription](t, op)

			yarequire.ProtoCmp(t, tt.wantSubscription, subscription, protocmp.IgnoreFields(&pb.Subscription{}, "created_at"))
		})
	}
}

func (suite *RwApiTestSuite) TestSubscriptions_List() {
	c := pb.NewSubscriptionServiceClient(suite.grpcClient)
	suite.addExternalOrgRole(suite.T(), suite.users.Kopatych, suite.orgs.Yandex42, iam.Roles.RepositoriesDeveloper)

	// create
	fixtures := []*struct {
		userIdentity entities.UserIdentity
		org          *entities.Organization
		subscription *pb.Subscription
	}{
		{
			userIdentity: testutils.UserIdentities.Kopatych,
			org:          suite.orgs.Yandex,
		},
		{
			userIdentity: testutils.UserIdentities.Kopatych,
			org:          suite.orgs.Yango,
		},
		{
			userIdentity: testutils.UserIdentities.Kopatych,
			org:          suite.orgs.Yandex42,
		},
		{
			userIdentity: testutils.UserIdentities.Krosh,
			org:          suite.orgs.Yandex,
		},
	}
	for _, fixture := range fixtures {
		op, err := c.UpdateSubscriptionType(testutils.AuthorizeGRPC(fixture.userIdentity), &pb.UpdateSubscriptionTypeRequest{
			Object: &pb.ObjectIdentity{
				Type: pb.ObjectIdentity_ORGANIZATION,
				Id:   grpc.MarshalID(fixture.org.ID),
			},
			Type: pb.Subscription_TYPE_NOTIFY,
		})
		require.NoError(suite.T(), err)

		fixture.subscription = testutils.UnmarshalGrpcResult[*pb.Subscription](suite.T(), op)
	}

	// create private, should not appear in List
	_, err := c.UpdateSubscriptionType(testutils.AuthorizeGRPC(testutils.UserIdentities.Krosh), &pb.UpdateSubscriptionTypeRequest{
		Object: &pb.ObjectIdentity{
			Type: pb.ObjectIdentity_REPOSITORY,
			Id:   grpc.MarshalID(suite.repos.History.ID),
		},
		Type: pb.Subscription_TYPE_NOTIFY,
	})
	require.NoError(suite.T(), err)
	require.NoError(suite.T(), suite.RepoRepo.UpdateRepositoryByID(suite.repos.History.ID).
		SetRepoVisibility(entities.Visibilities.Private).
		Commit(context.Background()))

	type args struct {
		ctx     context.Context
		request *pb.ListSubscriptionRequest
	}
	tests := []*struct {
		name     string
		args     args
		want     pb.ListSubscriptionResponse
		wantCode codes.Code
	}{
		{
			name: "ok first",
			args: args{
				ctx: testutils.AuthorizeGRPC(testutils.UserIdentities.Kopatych),
				request: &pb.ListSubscriptionRequest{
					PageSize: utils.PtrFromValue(uint64(1)),
				},
			},
			want: pb.ListSubscriptionResponse{
				Subscriptions: []*pb.Subscription{
					fixtures[0].subscription,
				},
				NextPageToken: testutils.Presence,
			},
		},
		{
			name: "ok all",
			args: args{
				ctx: testutils.AuthorizeGRPC(testutils.UserIdentities.Kopatych),
			},
			want: pb.ListSubscriptionResponse{
				Subscriptions: []*pb.Subscription{
					fixtures[0].subscription,
					fixtures[1].subscription,
					fixtures[2].subscription,
				},
			},
		},
		{
			name: "sorted",
			args: args{
				ctx: testutils.AuthorizeGRPC(testutils.UserIdentities.Kopatych),
				request: &pb.ListSubscriptionRequest{
					PageSize: utils.PtrFromValue(uint64(2)),
					SortBy: []*pagination_pb.SortOption{
						{
							Column:    "created_at",
							Direction: pagination_pb.SortOption_DESC,
						},
					},
				},
			},
			want: pb.ListSubscriptionResponse{
				Subscriptions: []*pb.Subscription{
					fixtures[2].subscription,
					fixtures[1].subscription,
				},
				NextPageToken: testutils.Presence,
			},
		},
		{
			name: "ok Krosh",
			args: args{
				ctx: testutils.AuthorizeGRPC(testutils.UserIdentities.Krosh),
			},
			want: pb.ListSubscriptionResponse{
				Subscriptions: []*pb.Subscription{
					fixtures[3].subscription,
				},
			},
		},
		{
			name: "ok empty",
			args: args{
				ctx: testutils.AuthorizeGRPC(testutils.UserIdentities.Barash),
			},
			want: pb.ListSubscriptionResponse{},
		},
	}

	for _, tt := range tests {
		suite.T().Run(tt.name, func(t *testing.T) {
			resp, err := c.List(tt.args.ctx, tt.args.request)

			if tt.wantCode != 0 {
				st, ok := status.FromError(err)
				require.True(t, ok)
				require.Equal(t, tt.wantCode, st.Code())
				return
			}

			require.NoError(t, err)

			// fill fields
			if tt.want.NextPageToken == testutils.Presence {
				require.NotEmpty(t, resp.NextPageToken)
				tt.want.NextPageToken = resp.NextPageToken
			}
			if tt.want.PrevPageToken == testutils.Presence {
				require.NotEmpty(t, resp.PrevPageToken)
				tt.want.PrevPageToken = resp.PrevPageToken
			}

			require.Len(t, resp.Subscriptions, len(tt.want.Subscriptions))
			for i := range resp.Subscriptions {
				require.NotEmpty(t, resp.Subscriptions[i].CreatedAt)
				tt.want.Subscriptions[i].CreatedAt = resp.Subscriptions[i].CreatedAt
			}

			yarequire.ProtoEqual(t, &tt.want, resp)
		})
	}

	suite.T().Run("pagination", func(t *testing.T) {
		ctx := testutils.AuthorizeGRPC(testutils.UserIdentities.Kopatych)

		// first
		resp, err := c.List(ctx, &pb.ListSubscriptionRequest{
			PageSize: utils.PtrFromValue(uint64(1)),
		})
		require.NoError(t, err)
		require.Len(t, resp.Subscriptions, 1)
		yarequire.ProtoEqual(t, fixtures[0].subscription.Object, resp.Subscriptions[0].Object)

		require.True(t, resp.NextPageToken != "")
		require.True(t, resp.PrevPageToken == "")

		// second + third
		resp, err = c.List(ctx, &pb.ListSubscriptionRequest{
			PageToken: &resp.NextPageToken,
			PageSize:  utils.PtrFromValue(uint64(2)),
		})
		require.NoError(t, err)
		require.Len(t, resp.Subscriptions, 2)
		yarequire.ProtoEqual(t, fixtures[1].subscription.Object, resp.Subscriptions[0].Object)
		yarequire.ProtoEqual(t, fixtures[2].subscription.Object, resp.Subscriptions[1].Object)

		require.True(t, resp.NextPageToken == "")
		require.True(t, resp.PrevPageToken != "")

		// prev
		resp, err = c.List(ctx, &pb.ListSubscriptionRequest{
			PageToken: &resp.PrevPageToken,
			PageSize:  utils.PtrFromValue(uint64(2)),
		})
		require.NoError(t, err)
		require.Len(t, resp.Subscriptions, 1)
		yarequire.ProtoEqual(t, fixtures[0].subscription.Object, resp.Subscriptions[0].Object)

		require.True(t, resp.NextPageToken != "")
		require.True(t, resp.PrevPageToken == "")
	})
}

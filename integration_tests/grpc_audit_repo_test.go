package integrationtests

import (
	"testing"

	"common/testutils/yarequire"
	"common/utils/rolesgenerator/iam"
	"gitcore/internal/entities"
	grpc_marshalling "gitcore/internal/grpcserver/marshalling"
	"gitcore/internal/testutils"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

func (suite *RwApiTestSuite) TestAuditEventForGrpcRepoServiceCreate() {
	t := suite.T()
	et := auditEventTest(suite.AuditEventsRepo)

	client := pb.NewRepoServiceClient(suite.grpcClient)

	projYandex, err := suite.MakeProject(suite.orgs.Yandex.Slug, "goodproj", entities.Visibilities.Public)
	require.NoError(t, err)

	projYandexStr := grpc_marshalling.IDInverse(projYandex)

	projSmeshariki, err := suite.MakeProject(suite.orgs.Smeshariki.Slug, "Smeshariki_goodproj", entities.Visibilities.Public)
	require.NoError(t, err)

	projSmesharikiStr := grpc_marshalling.IDInverse(projSmeshariki)

	federativeUser := suite.UserFixture(entities.UserIdentity{
		ID:  "federal1",
		Src: entities.IdentityProviders.IAM,
	}, entities.Visibilities.Private)
	suite.addOrgRole(t, federativeUser, suite.orgs.Smeshariki, iam.Roles.InternalOrganizationManagerMember)

	tt := map[string]struct {
		user           *entities.User
		request        *pb.CreateRepositoryRequest
		verifyError    bool
		expectedStatus codes.Code
	}{
		"repo created": {
			user: suite.users.Admin,
			request: &pb.CreateRepositoryRequest{
				Org: &pb.CreateRepositoryRequest_OrgId{
					OrgId: grpc_marshalling.IDInverse(suite.orgs.Yandex.ID),
				},
				Slug:        "foo",
				ProjectId:   &projYandexStr,
				Description: "Foo description",
				Visibility:  pb.ResourceVisibility_RESOURCE_UNSPECIFIED,
			},
		},
		"public repo created": {
			user: suite.users.Admin,
			request: &pb.CreateRepositoryRequest{
				Org: &pb.CreateRepositoryRequest_OrgId{
					OrgId: grpc_marshalling.IDInverse(suite.orgs.Yandex.ID),
				},
				Slug:        "foo2",
				ProjectId:   &projYandexStr,
				Description: "Foo description",
				Visibility:  pb.ResourceVisibility_RESOURCE_PUBLIC,
			},
		},
		"repo create error": {
			user: suite.users.Krosh,
			request: &pb.CreateRepositoryRequest{
				Org: &pb.CreateRepositoryRequest_OrgId{
					OrgId: grpc_marshalling.IDInverse(suite.orgs.Yandex.ID),
				},
				Slug:        "foo3",
				ProjectId:   &projYandexStr,
				Description: "Foo description",
				Visibility:  pb.ResourceVisibility_RESOURCE_PUBLIC,
			},
			expectedStatus: codes.PermissionDenied,
			verifyError:    true,
		},
		"repo created fed user": {
			user: federativeUser,
			request: &pb.CreateRepositoryRequest{
				Org: &pb.CreateRepositoryRequest_OrgId{
					OrgId: grpc_marshalling.IDInverse(suite.orgs.Smeshariki.ID),
				},
				Slug:        "foo4",
				ProjectId:   &projSmesharikiStr,
				Description: "Foo description",
				Visibility:  pb.ResourceVisibility_RESOURCE_UNSPECIFIED,
			},
		},
		"repo create error fed user": {
			user: federativeUser,
			request: &pb.CreateRepositoryRequest{
				Org: &pb.CreateRepositoryRequest_OrgId{
					OrgId: grpc_marshalling.IDInverse(suite.orgs.Yandex.ID),
				},
				Slug:        "foo5",
				ProjectId:   &projYandexStr,
				Description: "Foo description",
				Visibility:  pb.ResourceVisibility_RESOURCE_UNSPECIFIED,
			},
			expectedStatus: codes.PermissionDenied,
			verifyError:    true,
		},
	}

	for tn, tc := range tt {
		t.Run(tn, func(t *testing.T) {
			ctx := testutils.AuthorizeGRPC(tc.user.Identity)
			require.NoError(t, et.DeleteAuditEvents(ctx))

			_, err := client.Create(ctx, tc.request)
			yarequire.ProtoStatusEqual(t, tc.expectedStatus, err)

			msgs, err := et.GetCreateRepositoryAuditEvents(ctx)
			require.NoError(t, err)

			//yarequire.ProtoDumpFixture(t, msgs[0])
			yarequire.ProtoCompareWithFixture(t, msgs[0], et.ProtoCompareOpts()...)

			et.HasEventMetaDataOrganizationID(t, msgs[0])
		})
	}
}

func (suite *RwApiTestSuite) TestAuditEventForGrpcRepoServiceDelete() {
	t := suite.T()
	et := auditEventTest(suite.AuditEventsRepo)
	client := pb.NewRepoServiceClient(suite.grpcClient)

	tt := map[string]struct {
		user           *entities.User
		request        *pb.DeleteRepositoryRequest
		verifyError    bool
		expectedStatus codes.Code
	}{
		"repo deleted": {
			user:    suite.users.Admin,
			request: &pb.DeleteRepositoryRequest{Id: grpc_marshalling.IDInverse(suite.repos.Alpha.ID)},
		},
		"repo delete error": {
			user:           suite.users.Krosh,
			request:        &pb.DeleteRepositoryRequest{Id: grpc_marshalling.IDInverse(suite.repos.AlphaFork.ID)},
			expectedStatus: codes.PermissionDenied,
			verifyError:    true,
		},
	}

	for tn, tc := range tt {
		t.Run(tn, func(t *testing.T) {
			ctx := testutils.AuthorizeGRPC(tc.user.Identity)
			require.NoError(t, et.DeleteAuditEvents(ctx))

			_, err := client.Delete(ctx, tc.request)
			yarequire.ProtoStatusEqual(t, tc.expectedStatus, err)

			msgs, err := et.GetDeleteRepositoryAuditEvents(ctx)
			require.NoError(t, err)

			//yarequire.ProtoDumpFixture(t, msgs[0])
			yarequire.ProtoCompareWithFixture(t, msgs[0], et.ProtoCompareOpts()...)

			et.HasEventMetaDataOrganizationID(t, msgs[0])
		})
	}
}

func (suite *RwApiTestSuite) TestAuditEventForGrpcRepoUpdate() {
	t := suite.T()
	et := auditEventTest(suite.AuditEventsRepo)

	client := pb.NewRepoServiceClient(suite.grpcClient)

	tt := map[string]struct {
		user           *entities.User
		request        *pb.UpdateRepositoryRequest
		verifyError    bool
		expectedStatus codes.Code
	}{
		"repo update": {
			user: suite.users.Admin,
			request: &pb.UpdateRepositoryRequest{
				Id:          grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
				Description: "bbb",
				Visibility:  pb.ResourceVisibility_RESOURCE_PUBLIC,
				UpdateMask:  &fieldmaskpb.FieldMask{Paths: []string{"description", "visibility"}},
			},
		},
		"repo update permission error": {
			user: suite.users.Krosh,
			request: &pb.UpdateRepositoryRequest{
				Id:          grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
				Description: "bbb",
				Visibility:  pb.ResourceVisibility_RESOURCE_PUBLIC,
				UpdateMask:  &fieldmaskpb.FieldMask{Paths: []string{"description", "visibility"}},
			},
			expectedStatus: codes.PermissionDenied,
			verifyError:    true,
		},
	}

	for tn, tc := range tt {
		t.Run(tn, func(t *testing.T) {
			ctx := testutils.AuthorizeGRPC(tc.user.Identity)
			require.NoError(t, et.DeleteAuditEvents(ctx))

			_, err := client.Update(ctx, tc.request)
			yarequire.ProtoStatusEqual(t, tc.expectedStatus, err)

			msgs, err := et.GetUpdateRepositoryAuditEvents(ctx)
			require.NoError(t, err)

			//yarequire.ProtoDumpFixture(t, msgs[0])
			yarequire.ProtoCompareWithFixture(t, msgs[0], et.ProtoCompareOpts()...)

			et.HasEventMetaDataOrganizationID(t, msgs[0])
		})
	}
}

func (suite *RwApiTestSuite) TestAuditEventForGrpcRepoUpdateSlug() {
	t := suite.T()
	et := auditEventTest(suite.AuditEventsRepo)

	client := pb.NewRepoServiceClient(suite.grpcClient)

	repo1ID, _, _ := suite.makeRandomRepo(t, suite.users.Admin)
	repo2ID, _, _ := suite.makeRandomRepo(t, suite.users.Admin)

	tt := map[string]struct {
		user           *entities.User
		request        *pb.UpdateRepoSlugRequest
		verifyError    bool
		expectedStatus codes.Code
	}{
		"repo slug update": {
			user: suite.users.Admin,
			request: &pb.UpdateRepoSlugRequest{
				Id:   grpc_marshalling.IDInverse(repo1ID),
				Slug: "newslug1",
			},
		},
		"repo slug update permission error": {
			user: suite.users.Krosh,
			request: &pb.UpdateRepoSlugRequest{
				Id:   grpc_marshalling.IDInverse(repo2ID),
				Slug: "newslug2",
			},
			expectedStatus: codes.PermissionDenied,
			verifyError:    true,
		},
	}

	for tn, tc := range tt {
		t.Run(tn, func(t *testing.T) {
			ctx := testutils.AuthorizeGRPC(tc.user.Identity)
			require.NoError(t, et.DeleteAuditEvents(ctx))

			_, err := client.UpdateSlug(ctx, tc.request)
			yarequire.ProtoStatusEqual(t, tc.expectedStatus, err)

			msgs, err := et.GetUpdateRepositoryAuditEvents(ctx)
			require.NoError(t, err)

			//yarequire.ProtoDumpFixture(t, msgs[0])
			yarequire.ProtoCompareWithFixture(t, msgs[0], et.ProtoCompareOpts()...)

			et.HasEventMetaDataOrganizationID(t, msgs[0])
		})
	}
}

func (suite *RwApiTestSuite) TestAuditEventForGrpcRepoFork() {
	t := suite.T()
	et := auditEventTest(suite.AuditEventsRepo)

	client := pb.NewRepoServiceClient(suite.grpcClient)

	tt := map[string]struct {
		user           *entities.User
		request        *pb.ForkRepositoryRequest
		expectedStatus codes.Code
	}{
		"repo forked": {
			user: suite.users.Admin,
			request: &pb.ForkRepositoryRequest{
				Org: &pb.ForkRepositoryRequest_OrgId{
					OrgId: grpc_marshalling.IDInverse(suite.orgs.Yandex.ID),
				},
				ForkOriginId:      grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
				Slug:              "new-fork1",
				DefaultBranchOnly: true,
			},
		},
		"repo fork error": {
			user: suite.users.Krosh,
			request: &pb.ForkRepositoryRequest{
				Org: &pb.ForkRepositoryRequest_OrgId{
					OrgId: grpc_marshalling.IDInverse(suite.orgs.Yandex.ID),
				},
				ForkOriginId:      grpc_marshalling.IDInverse(suite.repos.Alpha.ID),
				Slug:              "new-fork2",
				DefaultBranchOnly: true,
			},
			expectedStatus: codes.PermissionDenied,
		},
	}

	for tn, tc := range tt {
		t.Run(tn, func(t *testing.T) {
			ctx := testutils.AuthorizeGRPC(tc.user.Identity)
			require.NoError(t, et.DeleteAuditEvents(ctx))

			_, err := client.Fork(ctx, tc.request)
			yarequire.ProtoStatusEqual(t, tc.expectedStatus, err)

			msgs, err := et.GetCreateRepositoryAuditEvents(ctx)
			require.NoError(t, err)

			//yarequire.ProtoDumpFixture(t, msgs[0])
			yarequire.ProtoCompareWithFixture(t, msgs[0], et.ProtoCompareOpts()...)

			et.HasEventMetaDataOrganizationID(t, msgs[0])
		})
	}
}

func (suite *RwApiTestSuite) TestAuditEventForGrpcRepoUpdateAccessBindings() {
	t := suite.T()
	et := auditEventTest(suite.AuditEventsRepo)

	client := pb.NewAccessBindingsServiceClient(suite.grpcClient)

	tt := map[string]struct {
		user           *entities.User
		request        *pb.UpdateAccessBindingsRequest
		expectedStatus codes.Code
	}{
		"repo bindings updated": {
			user: suite.users.Admin,
			request: &pb.UpdateAccessBindingsRequest{
				Object: &pb.Object{Identifier: &pb.Object_RepoId{RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID)}},
				BindingDeltas: []*pb.AccessBindingDelta{
					{
						Action: pb.DeltaAction_ADD,
						Binding: &pb.AccessBinding{
							Role: string(iam.Roles.Admin),
							Subject: &pb.Subject{
								Type: pb.Subject_USER,
								Id:   grpc_marshalling.IDInverse(suite.users.Krosh.ID),
							},
						},
					},
				},
			},
		},
		"repo bindings update error": {
			user: suite.users.Kopatych,
			request: &pb.UpdateAccessBindingsRequest{
				Object: &pb.Object{Identifier: &pb.Object_RepoId{RepoId: grpc_marshalling.IDInverse(suite.repos.Alpha.ID)}},
				BindingDeltas: []*pb.AccessBindingDelta{
					{
						Action: pb.DeltaAction_ADD,
						Binding: &pb.AccessBinding{
							Role: string(iam.Roles.Admin),
							Subject: &pb.Subject{
								Type: pb.Subject_USER,
								Id:   grpc_marshalling.IDInverse(suite.users.Krosh.ID),
							},
						},
					},
				},
			},
			expectedStatus: codes.PermissionDenied,
		},
	}

	for tn, tc := range tt {
		t.Run(tn, func(t *testing.T) {
			ctx := testutils.AuthorizeGRPC(tc.user.Identity)
			require.NoError(t, et.DeleteAuditEvents(ctx))

			_, err := client.UpdateAccessBindings(ctx, tc.request)
			yarequire.ProtoStatusEqual(t, tc.expectedStatus, err)

			msgs, err := et.GetUpdateRepositoryAccessBindingsAuditEvents(ctx)
			require.NoError(t, err)

			//yarequire.ProtoDumpFixture(t, msgs[0])
			yarequire.ProtoCompareWithFixture(t, msgs[0], et.ProtoCompareOpts()...)

			et.HasEventMetaDataOrganizationID(t, msgs[0])
		})
	}
}

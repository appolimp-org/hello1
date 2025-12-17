package integrationtests

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	fieldmaskpb "google.golang.org/protobuf/types/known/fieldmaskpb"

	"common/testutils/yarequire"
	"common/utils"
	"gitcore/internal/entities"
	"gitcore/internal/testutils"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
)

func (suite *RwApiTestSuite) TestAuditEventForGrpcCreatePersonalAccessToken() {
	t := suite.T()
	et := auditEventTest(suite.AuditEventsRepo)
	client := pb.NewMeServiceClient(suite.grpcClient)

	// create for name conflict
	_, err := client.CreatePAT(testutils.AuthorizeGRPC(suite.users.Kopatych.Identity), &pb.CreatePATRequest{
		Name:        "same_name",
		Description: utils.PtrFromValue("pat1"),
	})
	yarequire.ProtoStatusEqual(t, codes.OK, err)

	tt := map[string]struct {
		user           *entities.User
		request        *pb.CreatePATRequest
		expectedStatus codes.Code
		skipEvent      bool
	}{
		"happy_path": {
			user: suite.users.Kopatych,
			request: &pb.CreatePATRequest{
				Name:        "name",
				Description: utils.PtrFromValue("pat2"),
			},
		},
		"same_name": {
			user: suite.users.Kopatych,
			request: &pb.CreatePATRequest{
				Name:        "same_name",
				Description: utils.PtrFromValue("pat2"),
			},
		},
		"wrong_scope": {
			user: suite.users.Kopatych,
			request: &pb.CreatePATRequest{
				Name:        "same_name",
				Description: utils.PtrFromValue("pat2"),
				Scope: &pb.CreatePATRequest_Scope{
					RepoIds: []string{"1001"},
				},
			},
			expectedStatus: codes.NotFound,
		},
	}

	for tn, tc := range tt {
		t.Run(tn, func(t *testing.T) {
			ctx := testutils.AuthorizeGRPC(tc.user.Identity)
			require.NoError(t, et.DeleteAuditEvents(ctx))

			_, err := client.CreatePAT(ctx, tc.request)
			yarequire.ProtoStatusEqual(t, tc.expectedStatus, err)

			msgs, err := et.GetCreatePersonalAccessTokenAuditEvents(ctx)
			require.NoError(t, err)

			if tc.skipEvent {
				require.Empty(t, msgs)
				return
			}

			require.NotEmpty(t, msgs)

			//yarequire.ProtoDumpFixture(t, msgs[0])
			yarequire.ProtoCompareWithFixture(t, msgs[0], et.ProtoCompareOpts()...)

			et.HasEventMetaDataOrganizationID(t, msgs[0])
		})
	}
}

func (suite *RwApiTestSuite) TestAuditEventForGrpcUpdatePersonalAccessToken() {
	t := suite.T()
	et := auditEventTest(suite.AuditEventsRepo)
	client := pb.NewMeServiceClient(suite.grpcClient)

	pat1ID, _ := suite.createSimplePAT(t, suite.users.Kopatych)
	pat2ID, _ := suite.createSimplePAT(t, suite.users.Kopatych)
	pat3ID, _ := suite.createSimplePAT(t, suite.users.Kopatych)
	servicePatID, _ := suite.createServicePAT(t, suite.users.Kopatych)
	pikachuPatID, _ := suite.createSimplePAT(t, suite.users.Pikachu)

	tt := map[string]struct {
		user           *entities.User
		request        *pb.UpdatePATRequest
		expectedStatus codes.Code
		skipEvent      bool
	}{
		"happy_path": {
			user: suite.users.Kopatych,
			request: &pb.UpdatePATRequest{
				Id:   pat1ID,
				Name: utils.PtrFromValue("new name"),
			},
		},
		"update name": {
			user: suite.users.Kopatych,
			request: &pb.UpdatePATRequest{
				Id:   pat2ID,
				Name: utils.PtrFromValue("new name"),
				UpdateMask: &fieldmaskpb.FieldMask{
					Paths: []string{"name"},
				},
			},
		},
		"update description": {
			user: suite.users.Kopatych,
			request: &pb.UpdatePATRequest{
				Id:          pat3ID,
				Description: utils.PtrFromValue("new description"),
				UpdateMask: &fieldmaskpb.FieldMask{
					Paths: []string{"description"},
				},
			},
		},
		"can not update service pat": {
			user: suite.users.Kopatych,
			request: &pb.UpdatePATRequest{
				Id:          servicePatID,
				Description: utils.PtrFromValue("new description"),
				UpdateMask: &fieldmaskpb.FieldMask{
					Paths: []string{"description"},
				},
			},
			expectedStatus: codes.NotFound,
		},
		"can not update other user pat": {
			user: suite.users.Kopatych,
			request: &pb.UpdatePATRequest{
				Id:          pikachuPatID,
				Description: utils.PtrFromValue("new description"),
				UpdateMask: &fieldmaskpb.FieldMask{
					Paths: []string{"description"},
				},
			},
			expectedStatus: codes.NotFound,
		},
	}

	for tn, tc := range tt {
		t.Run(tn, func(t *testing.T) {
			ctx := testutils.AuthorizeGRPC(tc.user.Identity)
			require.NoError(t, et.DeleteAuditEvents(ctx))

			_, err := client.UpdatePAT(ctx, tc.request)
			yarequire.ProtoStatusEqual(t, tc.expectedStatus, err)

			msgs, err := et.GetUpdatePersonalAccessTokenAuditEvents(ctx)
			require.NoError(t, err)

			if tc.skipEvent {
				require.Empty(t, msgs)
				return
			}

			require.NotEmpty(t, msgs)

			//yarequire.ProtoDumpFixture(t, msgs[0])
			yarequire.ProtoCompareWithFixture(t, msgs[0], et.ProtoCompareOpts()...)

			et.HasEventMetaDataOrganizationID(t, msgs[0])
		})
	}
}

func (suite *RwApiTestSuite) TestAuditEventForGrpcDeletePersonalAccessToken() {
	t := suite.T()
	et := auditEventTest(suite.AuditEventsRepo)
	client := pb.NewMeServiceClient(suite.grpcClient)

	pat1ID, _ := suite.createSimplePAT(t, suite.users.Kopatych)
	pat2ID, _ := suite.createServicePAT(t, suite.users.Kopatych)
	pat3ID, _ := suite.createSimplePAT(t, suite.users.Pikachu)

	tt := map[string]struct {
		user           *entities.User
		request        *pb.DeletePATRequest
		expectedStatus codes.Code
		skipEvent      bool
	}{
		"happy_path": {
			user: suite.users.Kopatych,
			request: &pb.DeletePATRequest{
				Id: pat1ID,
			},
		},
		"unknown pat": {
			user: suite.users.Kopatych,
			request: &pb.DeletePATRequest{
				Id: "1001",
			},
			expectedStatus: codes.NotFound,
		},
		"can not delete service pat": {
			user: suite.users.Kopatych,
			request: &pb.DeletePATRequest{
				Id: pat2ID,
			},
			expectedStatus: codes.NotFound,
		},
		"can not delete other user pat": {
			user: suite.users.Kopatych,
			request: &pb.DeletePATRequest{
				Id: pat3ID,
			},
			expectedStatus: codes.NotFound,
		},
	}

	for tn, tc := range tt {
		t.Run(tn, func(t *testing.T) {
			ctx := testutils.AuthorizeGRPC(tc.user.Identity)
			require.NoError(t, et.DeleteAuditEvents(ctx))

			_, err := client.DeletePAT(ctx, tc.request)
			yarequire.ProtoStatusEqual(t, tc.expectedStatus, err)

			msgs, err := et.GetUpdatePersonalAccessTokenAuditEvents(ctx)
			require.NoError(t, err)

			if tc.skipEvent {
				require.Empty(t, msgs)
				return
			}

			require.NotEmpty(t, msgs)

			//yarequire.ProtoDumpFixture(t, msgs[0])
			yarequire.ProtoCompareWithFixture(t, msgs[0], et.ProtoCompareOpts()...)

			et.HasEventMetaDataOrganizationID(t, msgs[0])
		})
	}
}

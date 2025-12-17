package integrationtests

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"

	"common/testutils/yarequire"
	"gitcore/internal/entities"
	"gitcore/internal/testutils"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
)

func (suite *RwApiTestSuite) TestAuditEventForGrpcAddPersonalPublicSshKey() {
	t := suite.T()
	et := auditEventTest(suite.AuditEventsRepo)
	client := pb.NewMeServiceClient(suite.grpcClient)

	// create for name conflict
	_, err := client.CreatePublicSshKey(testutils.AuthorizeGRPC(suite.users.Kopatych.Identity), &pb.CreatePublicSshKeyRequest{
		Name:    "same_name",
		Content: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAraWyy4LmGGPnlT12aU51epB2QyKGMLYmML1tdxbVoE noname",
	})
	yarequire.ProtoStatusEqual(t, codes.OK, err)

	// create for key conflict
	_, err = client.CreatePublicSshKey(testutils.AuthorizeGRPC(suite.users.Pikachu.Identity), &pb.CreatePublicSshKeyRequest{
		Name:    "same_key",
		Content: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIEtf5Hi+vao/7GbSOMGUwr7OLM19xkYEac1cNye/7+Ot noname",
	})
	yarequire.ProtoStatusEqual(t, codes.OK, err)

	tt := map[string]struct {
		user           *entities.User
		request        *pb.CreatePublicSshKeyRequest
		expectedStatus codes.Code
		skipEvent      bool
	}{
		"happy_path": {
			user: suite.users.Kopatych,
			request: &pb.CreatePublicSshKeyRequest{
				Name:    "name",
				Content: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIMqtfkzKj2+k8QXwpeSPa0xvLP0w2YRbNPBXPXrlCf3Q john@doe.ed25519",
			},
		},
		"same_name": {
			user: suite.users.Kopatych,
			request: &pb.CreatePublicSshKeyRequest{
				Name:    "same_name",
				Content: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIKP61K9M5aElOEhC8R2KhC++rMjbvOpr6EJgv9omlmbT noname",
			},
		},
		"key_conflict": {
			user: suite.users.Kopatych,
			request: &pb.CreatePublicSshKeyRequest{
				Name:    "same_key",
				Content: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIEtf5Hi+vao/7GbSOMGUwr7OLM19xkYEac1cNye/7+Ot noname",
			},
			expectedStatus: codes.AlreadyExists,
		},
		"invalid_key": {
			user: suite.users.Kopatych,
			request: &pb.CreatePublicSshKeyRequest{
				Name:    "name",
				Content: "invalid/7+Ot noname",
			},
			expectedStatus: codes.InvalidArgument,
			skipEvent:      true,
		},
	}

	for tn, tc := range tt {
		t.Run(tn, func(t *testing.T) {
			ctx := testutils.AuthorizeGRPC(tc.user.Identity)
			require.NoError(t, et.DeleteAuditEvents(ctx))

			_, err := client.CreatePublicSshKey(ctx, tc.request)
			yarequire.ProtoStatusEqual(t, tc.expectedStatus, err)

			msgs, err := et.GetAddPersonalPublicSSHKeyAuditEvents(ctx)
			require.NoError(t, err)

			if tc.skipEvent {
				require.Empty(t, msgs)
				return
			}

			require.NotEmpty(t, msgs)

			// yarequire.ProtoDumpFixture(t, msgs[0])
			yarequire.ProtoCompareWithFixture(t, msgs[0], et.ProtoCompareOpts()...)

			et.HasEventMetaDataOrganizationID(t, msgs[0])
		})
	}
}

func (suite *RwApiTestSuite) TestAuditEventForGrpcRemovePersonalPublicSshKey() {
	t := suite.T()
	et := auditEventTest(suite.AuditEventsRepo)
	client := pb.NewMeServiceClient(suite.grpcClient)

	// one
	op, err := client.CreatePublicSshKey(testutils.AuthorizeGRPC(suite.users.Kopatych.Identity), &pb.CreatePublicSshKeyRequest{
		Name:    "name",
		Content: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAraWyy4LmGGPnlT12aU51epB2QyKGMLYmML1tdxbVoE noname",
	})
	yarequire.ProtoStatusEqual(t, codes.OK, err)
	resp := testutils.UnmarshalGrpcResult[*pb.PublicSshKey](t, op)
	kopatychKeyID := resp.GetId()

	// two
	op, err = client.CreatePublicSshKey(testutils.AuthorizeGRPC(suite.users.Pikachu.Identity), &pb.CreatePublicSshKeyRequest{
		Name:    "name",
		Content: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIEtf5Hi+vao/7GbSOMGUwr7OLM19xkYEac1cNye/7+Ot noname",
	})
	yarequire.ProtoStatusEqual(t, codes.OK, err)
	resp = testutils.UnmarshalGrpcResult[*pb.PublicSshKey](t, op)
	pikachuKeyID := resp.GetId()

	tt := map[string]struct {
		user           *entities.User
		request        *pb.DeletePublicSshKeyRequest
		expectedStatus codes.Code
		skipEvent      bool
	}{
		"happy_path": {
			user: suite.users.Kopatych,
			request: &pb.DeletePublicSshKeyRequest{
				Id: kopatychKeyID,
			},
		},
		"other_key": {
			user: suite.users.Kopatych,
			request: &pb.DeletePublicSshKeyRequest{
				Id: pikachuKeyID,
			},
			expectedStatus: codes.NotFound,
		},
		"not_found": {
			user: suite.users.Kopatych,
			request: &pb.DeletePublicSshKeyRequest{
				Id: "123123",
			},
			expectedStatus: codes.NotFound,
		},
	}

	for tn, tc := range tt {
		t.Run(tn, func(t *testing.T) {
			ctx := testutils.AuthorizeGRPC(tc.user.Identity)
			require.NoError(t, et.DeleteAuditEvents(ctx))

			_, err := client.DeletePublicSshKey(ctx, tc.request)
			yarequire.ProtoStatusEqual(t, tc.expectedStatus, err)

			msgs, err := et.GetRemovePersonalPublicSSHKeyAuditEvents(ctx)
			require.NoError(t, err)

			if tc.skipEvent {
				require.Empty(t, msgs)
				return
			}

			require.NotEmpty(t, msgs)

			// yarequire.ProtoDumpFixture(t, msgs[0])
			yarequire.ProtoCompareWithFixture(t, msgs[0], et.ProtoCompareOpts()...)

			et.HasEventMetaDataOrganizationID(t, msgs[0])
		})
	}
}

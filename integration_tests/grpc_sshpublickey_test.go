package integrationtests

import (
	"common/testutils/yarequire"
	"gitcore/internal/testutils"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	pagination_pb "private_api/generated/yandex/cloud/pagination"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"strings"
	"testing"
)

func (suite *RwApiTestSuite) TestCreatePublicKey_ValidationErrors() {
	t := suite.T()

	ctx := testutils.AuthorizeGRPC(suite.users.Slowpoke.Identity)
	c := pb.NewMeServiceClient(suite.grpcClient)

	tests := []struct {
		name      string
		request   *pb.CreatePublicSshKeyRequest
		wantCode  codes.Code
		wantCode2 codes.Code
	}{
		{
			name: "long string and blank",
			request: &pb.CreatePublicSshKeyRequest{
				Name:    strings.Repeat("a", 257),
				Content: "",
			},
			wantCode: codes.InvalidArgument,
		},
		{
			name: "unparsed",
			request: &pb.CreatePublicSshKeyRequest{
				Name:    "a name",
				Content: "sdfsdfsdfsdfsdf",
			},
			wantCode: codes.InvalidArgument,
		},
		{
			name: "too long ssh key",
			request: &pb.CreatePublicSshKeyRequest{
				Name:    "a name",
				Content: strings.Repeat("s", 64*1024),
			},
			wantCode: codes.InvalidArgument,
		},
		{
			name: "duplicated",
			request: &pb.CreatePublicSshKeyRequest{
				Name:    "good one",
				Content: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIBia7QxUtJzlaMOSoGz/MrgaeQnGaRTFTcR/6mEXg3Qz your_email@example.com",
			},
			wantCode:  codes.OK,
			wantCode2: codes.AlreadyExists,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := c.CreatePublicSshKey(ctx, tt.request)
			yarequire.ProtoStatusEqual(t, tt.wantCode, err)

			if tt.wantCode2 > 0 {
				_, err := c.CreatePublicSshKey(ctx, tt.request)
				yarequire.ProtoStatusEqual(t, tt.wantCode2, err)
			}
		})
	}
}

func (suite *RwApiTestSuite) TestPublicKey() {
	t := suite.T()

	ctx := testutils.AuthorizeGRPC(suite.users.Slowpoke.Identity)
	c := pb.NewMeServiceClient(suite.grpcClient)

	tests := []struct {
		Name    string
		Content string
	}{
		{"good", "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIMqtfkzKj2+k8QXwpeSPa0xvLP0w2YRbNPBXPXrlCf3Q john@doe.ed25519"},
		{"good rsa 1024", "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAAAgQDiBqhMVCRnmJlVdAklbBwnqC4P6OPtn5Hnia7fuQCmlEOKsXFe" +
			"gsEfmLSQbZXeoQk3E7f2WIK6QPXZRFHaDiilc8IH4fT1jpyqgWyxbtAlstlPmYprstgVqZTiQcOcvcI2MsPz36iY84qTZhaKEE89gu7HzWlNXI7sYUm3" +
			"/bQICQ== RSA 1024 bit Keys"},
		{"good rsa 4096", "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAACAQDrtdMsS/hQ3qhoagqckjeSNk9dpNiZVcV09f3IYU/PcjapQTJ5u" +
			"9igQsKMRUUwn0mS2RG/z+j8nxwVyWCP1bPKWN7DeMlEMcxGQfPcdhmBz/GLKZWdAgwGh1xGlW3yuP6a7fi4EFj5Ui+8LDL1ZZz3LGqa182tJpZ9KNSQte" +
			"ENntjqtjTd9P7aiWyUwFjYvfkbPPulVJy+pOOwLp6dgtvgvB++2ykOsMTN93JBZxa16TnLm5qpCadQ9omdO+3y2xQso704LzB2Z/iL6CS1WlJIloS3kCp" +
			"cZp0lSLb7px/m2M/jaZ4+WbXq5OgXZT8UF954VfYYjdkZwcabmIZb4pS9lRUrcHe56U2JcdfESETbQbwsbXGlLFxsAHrOvSeu0Q5hFDmNzq+jP8me8i0J" +
			"H5kXZdJJfCJiV7TZvzAO4Ie85KvGrcS7BIQTKpFzCzF7fmB2X5Ub7KcRdjBH0CzUQRQdbrrt3OQwLvajcrw1I9A2UX70ljfCFmV3Sl7JYKNmqTQIB/9Hn" +
			"L5KlvJx5GYPUQy2pSM0GQEJEEGNglNbciizQHKBvUq7BIROEGcZMPeqoH0Xqxc+j7WB6fUTq85erRCEn9AU3OOkRUPFu59Xy+PNV3vCE3oqcXbOgZmJVl" +
			"waz1MHwo6qPNgNNswkgA0bzC/468YjEY9LIfdm1EukZlaQOw== RSA 4096 bit Keys"},
	}

	for _, tt := range tests {
		req := &pb.CreatePublicSshKeyRequest{
			Name:    tt.Name,
			Content: tt.Content,
		}

		op, err := c.CreatePublicSshKey(ctx, req)
		require.NoError(t, err)

		resp := testutils.UnmarshalGrpcResult[*pb.PublicSshKey](t, op)
		require.Equal(t, tt.Name, resp.Name)
	}

	res, err := c.ListPublicSshKeys(ctx, &pb.ListPublicSshKeysRequest{})
	require.NoError(t, err)
	require.Equal(t, 3, len(res.Keys))

	res, err = c.ListPublicSshKeys(ctx, &pb.ListPublicSshKeysRequest{SortBy: []*pagination_pb.SortOption{
		{Column: "name", Direction: pagination_pb.SortOption_ASC},
	}})
	require.NoError(t, err)
	require.Equal(t, 3, len(res.Keys))

	require.Equal(t, tests[0].Name, res.Keys[0].Name)
	require.Equal(t, tests[1].Name, res.Keys[1].Name)
	require.Equal(t, tests[2].Name, res.Keys[2].Name)

	op, _ := c.DeletePublicSshKey(ctx, &pb.DeletePublicSshKeyRequest{
		Id: res.Keys[0].Id,
	})

	resp := testutils.UnmarshalGrpcResult[*pb.PublicSshKey](t, op)
	require.Equal(t, res.Keys[0].Name, resp.Name)

	res, err = c.ListPublicSshKeys(ctx, &pb.ListPublicSshKeysRequest{SortBy: []*pagination_pb.SortOption{
		{Column: "name", Direction: pagination_pb.SortOption_ASC},
	}})
	require.NoError(t, err)
	require.Equal(t, 2, len(res.Keys))

	require.Equal(t, tests[1].Name, res.Keys[0].Name)
	require.Equal(t, tests[2].Name, res.Keys[1].Name)
}

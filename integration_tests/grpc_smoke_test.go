package integrationtests

import (
	"common/testutils/assertjson"
	valutils "common/valium/utils"
	"context"
	"gitcore/internal/testutils"
	errpb "private_api/generated/yandex/cloud"
	pb "private_api/generated/yandex/cloud/priv/gitcore/v1"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

func TestFieldMask(t *testing.T) {
	fs := &pb.UpdateOrgProfileRequest{DisplayName: "aaa", Location: &pb.Location{
		Country: "a",
		City:    "b",
	}}

	fm, _ := fieldmaskpb.New(fs, "display_name", "location")
	fs.UpdateMask = fm

	fmmap, _ := valutils.NewMessageMask(fs)
	require.True(t, fmmap.FieldProvided("display_name"))
	require.False(t, fmmap.FieldProvided("links"))

}

func (suite *RepoApiTestSuite) TestSmokeGRPC() {
	ctx := testutils.AuthorizeGRPC(testutils.UserIdentities.Kopatych)
	ctxAnon := context.Background()

	c := pb.NewGreeterServiceClient(suite.grpcClient)
	suite.T().Run("SayHello as Authenticated", func(t *testing.T) {
		name := "xxx"
		r, err := c.Hello(ctx, &pb.HelloRequest{Name: name, PageSize: 10})
		if err != nil {
			t.Fatalf("could not greet: %v", err)
		}
		require.Equal(t, testutils.UserIdentities.Kopatych.String(), r.Subject)
	})

	suite.T().Run("SayHello as Guest", func(t *testing.T) {
		name := "xxx"
		_, err := c.Hello(ctxAnon, &pb.HelloRequest{Name: name, PageSize: 10})
		require.Equal(t, codes.OK, status.Code(err))
	})

	suite.T().Run("Validation Fail", func(t *testing.T) {
		name := "xxx"
		_, err := c.Hello(ctx, &pb.HelloRequest{Name: name, PageSize: 1000500})
		require.Equal(t, codes.InvalidArgument, status.Code(err))
		st := status.Convert(err)
		d := st.Details()
		br, ok := d[0].(*errpb.BadRequest)
		require.True(t, ok)
		fv := br.GetFieldViolations()

		require.Equal(t, "page_size", fv[0].GetField())
		require.Equal(t, "Range", fv[0].GetDisplayMessage().GetMessageId())
		require.Equal(t, "value must be inside range [1, 1000]", fv[0].GetDisplayMessage().Fallback)
	})

	suite.T().Run("Slug Fail", func(t *testing.T) {
		_, err := c.Hello(ctx, &pb.HelloRequest{Name: "neofelis", PageSize: 1})
		require.Equal(t, codes.InvalidArgument, status.Code(err))
		st := status.Convert(err)
		d := st.Details()
		br, ok := d[0].(*errpb.BadRequest)
		require.True(t, ok)
		t.Log(assertjson.PrettyPrint(st.Details()))
		fv := br.GetFieldViolations()
		require.Equal(t, "name", fv[0].GetField())
		require.Equal(t, "Slug.Occupied", fv[0].GetDisplayMessage().GetMessageId())
	})

	suite.T().Run("Not Found", func(t *testing.T) {
		_, err := c.Hello(ctx, &pb.HelloRequest{Name: "missing", PageSize: 1})
		require.Equal(t, codes.NotFound, status.Code(err))
		st := status.Convert(err)
		t.Log(assertjson.PrettyPrint(st.Details()))
	})

	suite.T().Run("Deadline", func(t *testing.T) {
		_, err := c.Hello(ctx, &pb.HelloRequest{Name: "deadline", PageSize: 1})
		require.Equal(t, codes.DeadlineExceeded, status.Code(err))
	})
}

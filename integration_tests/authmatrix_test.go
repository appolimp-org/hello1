package integrationtests

import (
	"common/functools"
	"context"
	"fmt"
	"gitcore/internal/entities"
	"gitcore/internal/testutils"
	"github.com/go-resty/resty/v2"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"strings"
	"testing"
)

type codeMap struct {
	m        map[codes.Code][]string
	t        *testing.T
	everyone []string
}

func (c codeMap) String() string {
	var b strings.Builder
	for k, v := range c.m {
		b.WriteString(fmt.Sprintf("%v: %v\n", k, strings.Join(v, ", ")))
	}
	return b.String()
}

func (c codeMap) NoExceptions() {
	// TODO: check for Bad response codes
}

func (c codeMap) AllUsers() {
	c.NoExceptions()
	v := c.m[codes.OK]
	require.ElementsMatchf(c.t, v, c.everyone, "AllUsers must have access, got %s instead", c.String())
}

func (c codeMap) AllUsersBut(diff ...*entities.User) {
	var only []string
outer:
	for _, user := range c.everyone {
		for _, userExclude := range diff {
			if user == userExclude.Username {
				continue outer
			}
		}
		only = append(only, user)
	}
	c.specificUsers(only...)
}

func (c codeMap) SpecificUsers(only ...*entities.User) {
	c.specificUsers(functools.Map(only, func(t *entities.User) string {
		return t.Username
	})...)
}

func (c codeMap) specificUsers(only ...string) {
	c.NoExceptions()
	v := c.m[codes.OK]
	require.ElementsMatchf(c.t, v, only, "%s must have access, got %s instead", strings.Join(only, ", "), c.String())
}

func (suite *IntegrationTestSuite) AuthMatrix(t *testing.T, act func(ctx context.Context) error) codeMap {
	agg := codeMap{
		m: make(map[codes.Code][]string),
		t: t,
		everyone: functools.Map(suite.AuthMatrixUsers, func(t *entities.User) string {
			return t.Username
		}),
	}

	for _, user := range suite.AuthMatrixUsers {
		ctx := testutils.AuthorizeGRPC(user.Identity)
		err := act(ctx)
		st := status.Convert(err)
		v := agg.m[st.Code()]
		agg.m[st.Code()] = append(v, user.Username)
	}

	return agg
}

func (suite *IntegrationTestSuite) AuthMatrixHTTP(t *testing.T, act func(user *entities.User) (*resty.Response, error)) codeMap {
	agg := codeMap{
		m: make(map[codes.Code][]string),
		t: t,
		everyone: functools.Map(suite.AuthMatrixUsers, func(t *entities.User) string {
			return t.Username
		}),
	}

	for _, user := range suite.AuthMatrixUsers {
		response, err := act(user)

		require.NoError(t, err)
		code := HTTPStatusToGRPCCode(response.StatusCode())
		v := agg.m[code]
		agg.m[code] = append(v, user.Username)
	}

	return agg
}

func HTTPStatusToGRPCCode(status int) codes.Code {
	if status >= 500 {
		return codes.Internal
	}
	if status == 404 {
		return codes.NotFound
	}
	if status == 403 {
		return codes.PermissionDenied
	}
	if status == 401 {
		return codes.Unauthenticated
	}
	if status == 200 || status == 201 {
		return codes.OK
	}
	if status == 400 {
		return codes.FailedPrecondition
	}
	if status == 409 {
		return codes.AlreadyExists
	}
	return codes.Unknown
}

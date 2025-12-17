package integrationtests

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func (suite *RepoApiTestSuite) TestDocs() {
	t := suite.T()

	tests := []struct {
		name                string
		url                 string
		expectedCode        int
		expectedContentType string
	}{
		{
			name:                "docs",
			url:                 "/docs",
			expectedCode:        200,
			expectedContentType: "text/html; charset=utf-8",
		},
		{
			name:                "slash",
			url:                 "/docs/",
			expectedCode:        200,
			expectedContentType: "text/html; charset=utf-8",
		},
		{
			name:                "not_found",
			url:                 "/docs/wrong",
			expectedCode:        404,
			expectedContentType: "text/plain; charset=utf-8",
		},
		{
			name:                "json",
			url:                 "/docs/doc.json",
			expectedCode:        200,
			expectedContentType: "application/json; charset=utf-8",
		},
		{
			name:                "yaml",
			url:                 "/docs/doc.yaml",
			expectedCode:        200,
			expectedContentType: "text/plain; charset=utf-8",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := suite.client.R().
				Get(tt.url)

			require.NoError(t, err)
			require.Equal(t, tt.expectedCode, resp.StatusCode())

			contentType := resp.Header().Get("Content-Type")
			require.Equal(t, tt.expectedContentType, contentType)
		})
	}

}

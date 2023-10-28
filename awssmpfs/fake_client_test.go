package awssmpfs

import (
	"context"
	"fmt"
	"math/rand"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/aws/aws-sdk-go-v2/service/ssm/types"
)

type fakeClient struct {
	t       *testing.T
	params  map[string]*testVal
	getErr  error
	listErr error
}

var _ SSMClient = (*fakeClient)(nil)

func (c *fakeClient) GetParameter(
	_ context.Context, params *ssm.GetParameterInput, _ ...func(*ssm.Options),
) (*ssm.GetParameterOutput, error) {
	c.t.Helper()

	if c.getErr != nil {
		return nil, c.getErr
	}

	name := *params.Name
	if val, ok := c.params[name]; ok {
		out := ssm.GetParameterOutput{
			Parameter: &types.Parameter{
				Name:  params.Name,
				Value: &val.s,
				Type:  types.ParameterTypeString,
			},
		}

		return &out, nil
	}

	return nil, &types.ParameterNotFound{
		Message: aws.String("Simple Systems Manager can't find the specified parameter."),
	}
}

//nolint:funlen,gocyclo
func (c *fakeClient) GetParametersByPath(
	_ context.Context, params *ssm.GetParametersByPathInput, _ ...func(*ssm.Options),
) (out *ssm.GetParametersByPathOutput, err error) {
	c.t.Helper()

	if *params.Path == "//" {
		panic("wat")
	}

	if c.listErr != nil {
		return nil, c.listErr
	}

	offset := 0
	if params.NextToken != nil {
		offset, err = strconv.Atoi(*params.NextToken)
		if err != nil {
			return nil, fmt.Errorf("invalid nextToken for fakeClient %q: %w", *params.NextToken, err)
		}
	}

	paramList := []types.Parameter{}

	for k := range c.params {
		cond := strings.HasPrefix(k, *params.Path)

		if cond {
			paramList = append(paramList, types.Parameter{
				Name: aws.String(k),
			})
		}
	}

	// sort so pagination works
	sort.Slice(paramList, func(i, j int) bool {
		return aws.ToString(paramList[i].Name) < aws.ToString(paramList[j].Name)
	})

	if params.MaxResults == nil || params.MaxResults == aws.Int32(0) {
		// default to 2 results so we trigger pagination
		params.MaxResults = aws.Int32(2)
	}

	l := len(paramList)
	m := int(*params.MaxResults)

	high := offset + m

	var nextToken *string

	switch {
	case high < l:
		paramList = paramList[offset:high]
		nextToken = aws.String(strconv.Itoa(high))
	case offset < l:
		paramList = paramList[offset:]
	default:
		paramList = nil
	}

	// un-sort for a slightly more realistic test
	rand.Shuffle(len(paramList), func(i, j int) {
		paramList[i], paramList[j] = paramList[j], paramList[i]
	})

	return &ssm.GetParametersByPathOutput{
		Parameters: paramList,
		NextToken:  nextToken,
	}, nil
}

func clientWithValues(t *testing.T, params map[string]*testVal, errs ...error) *fakeClient {
	t.Helper()

	c := &fakeClient{t: t, params: params}

	switch len(errs) {
	case 1:
		c.getErr = errs[0]
	case 2:
		c.getErr = errs[0]
		c.listErr = errs[1]
	}

	return c
}

type testVal struct {
	t types.ParameterType
	s string
}

func vs(s string) *testVal {
	return &testVal{s: s, t: types.ParameterTypeString}
}

func vss(s string) *testVal {
	return &testVal{s: s, t: types.ParameterTypeSecureString}
}

func vl(s string) *testVal {
	return &testVal{s: s, t: types.ParameterTypeStringList}
}

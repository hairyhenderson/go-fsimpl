package awssmfs

import (
	"context"
	"fmt"
	"math/rand"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"
)

type fakeClient struct {
	t       *testing.T
	secrets map[string]*testVal
	getErr  error
	listErr error
}

var _ SecretsManagerClient = (*fakeClient)(nil)

func (c *fakeClient) GetSecretValue(ctx context.Context, params *secretsmanager.GetSecretValueInput,
	optFns ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error) {
	c.t.Helper()

	if c.getErr != nil {
		return nil, c.getErr
	}

	name := *params.SecretId
	if val, ok := c.secrets[name]; ok {
		out := secretsmanager.GetSecretValueOutput{Name: aws.String(name)}

		if val.b != nil {
			out.SecretBinary = make([]byte, len(val.b))
			copy(out.SecretBinary, val.b)
		} else {
			out.SecretString = aws.String(val.s)
		}

		return &out, nil
	}

	return nil, &types.ResourceNotFoundException{
		Message: aws.String("Secrets Manager can't find the specified secret."),
	}
}

//nolint:funlen,gocyclo
func (c *fakeClient) ListSecrets(ctx context.Context, params *secretsmanager.ListSecretsInput,
	optFns ...func(*secretsmanager.Options)) (out *secretsmanager.ListSecretsOutput, err error) {
	c.t.Helper()

	if c.listErr != nil {
		return nil, c.listErr
	}

	nameFilter := ""

	for _, f := range params.Filters {
		if f.Key == "name" {
			nameFilter = f.Values[0]

			break
		}
	}

	offset := 0
	if params.NextToken != nil {
		offset, err = strconv.Atoi(*params.NextToken)
		if err != nil {
			return nil, fmt.Errorf("invalid nextToken for fakeClient %q: %w", *params.NextToken, err)
		}
	}

	secretList := []types.SecretListEntry{}

	for k := range c.secrets {
		cond := strings.HasPrefix(k, nameFilter)
		if strings.HasPrefix(nameFilter, "!") {
			cond = !strings.HasPrefix(k, nameFilter[1:])
		}

		if cond {
			secretList = append(secretList, types.SecretListEntry{
				Name: aws.String(k),
			})
		}
	}

	// sort so pagination works
	sort.Slice(secretList, func(i, j int) bool {
		return aws.ToString(secretList[i].Name) < aws.ToString(secretList[j].Name)
	})

	if params.MaxResults == 0 {
		// default to 2 results so we trigger pagination
		params.MaxResults = 2
	}

	l := len(secretList)
	m := int(params.MaxResults)

	high := offset + m

	var nextToken *string

	switch {
	case high < l:
		secretList = secretList[offset:high]
		nextToken = aws.String(strconv.Itoa(high))
	case offset < l:
		secretList = secretList[offset:]
	default:
		secretList = nil
	}

	// un-sort for a slightly more realistic test
	rand.Seed(time.Now().UnixNano())
	rand.Shuffle(len(secretList), func(i, j int) {
		secretList[i], secretList[j] = secretList[j], secretList[i]
	})

	return &secretsmanager.ListSecretsOutput{
		SecretList: secretList,
		NextToken:  nextToken,
	}, nil
}

func clientWithValues(t *testing.T, secrets map[string]*testVal, errs ...error) *fakeClient {
	t.Helper()

	c := &fakeClient{t: t, secrets: secrets}

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
	s string
	b []byte
}

func vs(s string) *testVal {
	return &testVal{s: s}
}

func vb(b []byte) *testVal {
	return &testVal{b: b}
}

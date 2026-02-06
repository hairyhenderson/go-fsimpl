package gcpsmfs

import (
	"context"
	"strings"

	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	"github.com/googleapis/gax-go/v2"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type mockClient struct {
	secrets map[string][]byte
	err     error
}

func (m *mockClient) AccessSecretVersion(
	_ context.Context,
	req *secretmanagerpb.AccessSecretVersionRequest,
	_ ...gax.CallOption,
) (*secretmanagerpb.AccessSecretVersionResponse, error) {
	if m.err != nil {
		return nil, m.err
	}

	val, ok := m.secrets[req.Name]
	if !ok {
		return nil, status.Error(codes.NotFound, "secret not found")
	}

	return &secretmanagerpb.AccessSecretVersionResponse{
		Name: req.Name,
		Payload: &secretmanagerpb.SecretPayload{
			Data: val,
		},
	}, nil
}

func (m *mockClient) GetSecretVersion(
	_ context.Context,
	_ *secretmanagerpb.GetSecretVersionRequest,
	_ ...gax.CallOption,
) (*secretmanagerpb.SecretVersion, error) {
	return nil, nil
}

func (m *mockClient) ListSecrets(
	_ context.Context,
	req *secretmanagerpb.ListSecretsRequest,
	_ ...gax.CallOption,
) SecretIterator {
	if m.err != nil {
		return &mockIterator{err: m.err}
	}

	var secrets []*secretmanagerpb.Secret
	// We expect req.Parent to be "projects/{project}"
	// Secrets keys are "projects/{project}/secrets/{secret}/versions/latest"
	// We want to return "projects/{project}/secrets/{secret}"

	seen := map[string]bool{}

	for k := range m.secrets {
		if strings.HasPrefix(k, req.Parent+"/secrets/") {
			// Extract secret name (remove /versions/...)
			parts := strings.Split(k, "/")
			// k is projects/p/secrets/s/versions/v
			// we want projects/p/secrets/s
			if len(parts) >= 4 {
				secretName := strings.Join(parts[:4], "/")
				if !seen[secretName] {
					secrets = append(secrets, &secretmanagerpb.Secret{
						Name: secretName,
					})
					seen[secretName] = true
				}
			}
		}
	}

	return &mockIterator{secrets: secrets}
}

type mockIterator struct {
	err     error
	secrets []*secretmanagerpb.Secret
	index   int
}

func (it *mockIterator) Next() (*secretmanagerpb.Secret, error) {
	if it.err != nil {
		return nil, it.err
	}

	if it.index >= len(it.secrets) {
		return nil, iterator.Done
	}

	s := it.secrets[it.index]
	it.index++

	return s, nil
}

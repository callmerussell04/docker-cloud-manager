package depstest_test

import (
	"errors"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/depstest"
)

type store interface{ Get() string }

type memoryStore struct{}

func (*memoryStore) Get() string { return "value" }

type valueStore struct{}

func (valueStore) Get() string { return "value" }

type deps struct {
	Store     store
	Values    store
	Authority string
}

type thing struct{}

func newThing(d deps) (*thing, error) {
	if isNil(d.Store) {
		return nil, errors.New("thing: store is required")
	}
	if isNil(d.Values) {
		return nil, errors.New("thing: values is required")
	}
	if d.Authority == "" {
		return nil, errors.New("thing: authority is required")
	}
	return &thing{}, nil
}

func isNil(s store) bool {
	switch v := s.(type) {
	case nil:
		return true
	case *memoryStore:
		return v == nil
	case *valueStore:
		return v == nil
	}
	return false
}

func validDeps(*testing.T) deps {
	return deps{Store: &memoryStore{}, Values: valueStore{}, Authority: "registry:5000"}
}

func TestRequireRejectsMissing(t *testing.T) {
	depstest.RequireRejectsMissing(t, validDeps, newThing, map[string]string{
		"Store":     "store",
		"Values":    "values",
		"Authority": "authority",
	})
}

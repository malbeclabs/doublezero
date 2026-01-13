package bgp

import (
	"errors"
	"testing"

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
	"github.com/stretchr/testify/require"
)

type MockRouteReaderWriter struct {
	RouteAddFunc        func(*routing.Route) error
	RouteDeleteFunc     func(*routing.Route) error
	RouteByProtocolFunc func(int) ([]*routing.Route, error)
}

func (m *MockRouteReaderWriter) RouteAdd(route *routing.Route) error {
	if m.RouteAddFunc == nil {
		return nil
	}
	return m.RouteAddFunc(route)
}

func (m *MockRouteReaderWriter) RouteDelete(route *routing.Route) error {
	if m.RouteDeleteFunc == nil {
		return nil
	}
	return m.RouteDeleteFunc(route)
}

func (m *MockRouteReaderWriter) RouteByProtocol(protocol int) ([]*routing.Route, error) {
	if m.RouteByProtocolFunc == nil {
		return nil, nil
	}
	return m.RouteByProtocolFunc(protocol)
}

func TestClient_RouteReaderWriterWithNoUninstall_NoUninstallTrue_SuppressesDeleteOnly(t *testing.T) {
	t.Parallel()

	var addCalled, deleteCalled bool
	addErr := errors.New("add error")

	underlying := &MockRouteReaderWriter{
		RouteAddFunc: func(*routing.Route) error {
			addCalled = true
			return addErr
		},
		RouteDeleteFunc: func(*routing.Route) error {
			deleteCalled = true
			return errors.New("should not be called")
		},
	}

	wrapped := newRouteReaderWriterWithNoUninstall(underlying, true)
	route := &routing.Route{}

	// RouteAdd should always delegate, even when noUninstall=true
	err := wrapped.RouteAdd(route)
	require.Error(t, err)
	require.Equal(t, addErr, err)
	require.True(t, addCalled, "underlying RouteAddFunc should be called even when noUninstall=true")

	// RouteDelete should be suppressed when noUninstall=true
	err = wrapped.RouteDelete(route)
	require.NoError(t, err, "RouteDelete should be suppressed and return nil when noUninstall=true")
	require.False(t, deleteCalled, "underlying RouteDeleteFunc should not be called when noUninstall=true")
}

func TestClient_RouteReaderWriterWithNoUninstall_NoUninstallFalse_DelegatesAddDelete(t *testing.T) {
	t.Parallel()

	addErr := errors.New("add failed")
	deleteErr := errors.New("delete failed")

	var addCalled, deleteCalled bool

	underlying := &MockRouteReaderWriter{
		RouteAddFunc: func(*routing.Route) error {
			addCalled = true
			return addErr
		},
		RouteDeleteFunc: func(*routing.Route) error {
			deleteCalled = true
			return deleteErr
		},
	}

	wrapped := newRouteReaderWriterWithNoUninstall(underlying, false)
	route := &routing.Route{}

	err := wrapped.RouteAdd(route)
	require.Error(t, err)
	require.Equal(t, addErr, err)
	require.True(t, addCalled, "underlying RouteAddFunc should be called when noUninstall=false")

	err = wrapped.RouteDelete(route)
	require.Error(t, err)
	require.Equal(t, deleteErr, err)
	require.True(t, deleteCalled, "underlying RouteDeleteFunc should be called when noUninstall=false")
}

func TestClient_RouteReaderWriterWithNoUninstall_RouteByProtocol_DelegatesRegardlessOfFlag(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		noUninstall bool
	}{
		{"noUninstall_true", true},
		{"noUninstall_false", false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			wantRoutes := []*routing.Route{{}, {}}
			wantErr := errors.New("by-proto failed")

			var called bool
			var gotProto int

			underlying := &MockRouteReaderWriter{
				RouteByProtocolFunc: func(p int) ([]*routing.Route, error) {
					called = true
					gotProto = p
					return wantRoutes, wantErr
				},
			}

			wrapped := newRouteReaderWriterWithNoUninstall(underlying, tc.noUninstall)

			routes, err := wrapped.RouteByProtocol(42)

			require.True(t, called, "underlying RouteByProtocolFunc should always be called")
			require.Equal(t, 42, gotProto, "protocol argument should be forwarded")
			require.Equal(t, wantRoutes, routes, "routes should be forwarded from underlying")
			require.Equal(t, wantErr, err, "error should be forwarded from underlying")
		})
	}
}

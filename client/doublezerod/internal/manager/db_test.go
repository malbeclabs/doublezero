package manager_test

import (
	"log"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/api"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/manager"
)

func TestDbNew(t *testing.T) {
	tests := []struct {
		name          string
		setupStateDir bool
		goldenFile    string
		state         []*api.ProvisionRequest
		expectError   bool
	}{
		{
			name:          "state_dir_does_not_exist",
			setupStateDir: false,
			goldenFile:    "",
			expectError:   true,
		},
		{
			name:          "read_edgefiltering_state_file",
			setupStateDir: true,
			goldenFile:    "./fixtures/doublezerod.edgefiltering.json",
			state: []*api.ProvisionRequest{
				{
					TunnelSrc:    net.IP{1, 1, 1, 1},
					TunnelDst:    net.IP{2, 2, 2, 2},
					TunnelNet:    &net.IPNet{IP: net.IP{169, 254, 0, 0}, Mask: net.IPMask{255, 255, 255, 254}},
					DoubleZeroIP: net.IP{7, 7, 7, 7},
					DoubleZeroPrefixes: []*net.IPNet{
						{IP: net.IP{7, 0, 0, 0}, Mask: net.IPMask{255, 0, 0, 0}},
					},
					BgpLocalAsn:  65000,
					BgpRemoteAsn: 65001,
					UserType:     api.UserTypeEdgeFiltering,
				},
			},
			expectError: false,
		},
		{
			name:          "read_ibrl_state_file",
			setupStateDir: true,
			goldenFile:    "./fixtures/doublezerod.ibrl.json",
			state: []*api.ProvisionRequest{
				{
					TunnelSrc:          net.IP{1, 1, 1, 1},
					TunnelDst:          net.IP{2, 2, 2, 2},
					TunnelNet:          &net.IPNet{IP: net.IP{169, 254, 0, 0}, Mask: net.IPMask{255, 255, 255, 254}},
					DoubleZeroIP:       net.IP{1, 1, 1, 1},
					DoubleZeroPrefixes: nil,
					BgpLocalAsn:        65000,
					BgpRemoteAsn:       65001,
					UserType:           api.UserTypeIBRL,
				},
			},
			expectError: false,
		},
		{
			name:          "missing_state_file",
			setupStateDir: true,
			goldenFile:    "",
			state:         nil,
			expectError:   false,
		},
		{
			name:          "read_old_style_state_file",
			setupStateDir: true,
			goldenFile:    "./fixtures/doublezerod.old.style.json",
			state: []*api.ProvisionRequest{
				{
					TunnelSrc:          net.IP{1, 1, 1, 1},
					TunnelDst:          net.IP{2, 2, 2, 2},
					TunnelNet:          &net.IPNet{IP: net.IP{169, 254, 0, 0}, Mask: net.IPMask{255, 255, 255, 254}},
					DoubleZeroIP:       net.IP{1, 1, 1, 1},
					DoubleZeroPrefixes: nil,
					BgpLocalAsn:        65000,
					BgpRemoteAsn:       65001,
					UserType:           api.UserTypeIBRL,
				},
			},
			expectError: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root, err := os.MkdirTemp("", "doublezerod")
			if err != nil {
				t.Fatalf("error creating temp dir: %v", err)
			}
			defer os.RemoveAll(root)

			// XDG_STATE_HOME is used in NewDb so use it to set a tmp dir
			t.Setenv("XDG_STATE_HOME", root)

			path := filepath.Join(root, "doublezerod")
			if test.setupStateDir {
				if err := os.Mkdir(path, 0766); err != nil {
					t.Fatalf("error creating state dir: %v", err)
				}
			}

			if test.goldenFile != "" {
				gold, err := os.ReadFile(test.goldenFile)
				if err != nil {
					t.Fatalf("error reading goldenfile: %v", err)
				}
				err = os.WriteFile(filepath.Join(path, "doublezerod.json"), gold, 0666)
				if err != nil {
					t.Fatalf("error copying goldenfile: %v", err)
				}
			}

			db, err := manager.NewDb()
			if test.expectError && err == nil {
				t.Fatalf("expected error but got none")
			}
			if !test.expectError && err != nil {
				t.Fatalf("error creating db file: %v", err)
			}
			if !test.expectError {
				if _, err := os.Stat(db.Path); err != nil {
					t.Fatalf("could not find db file: %v", err)
				}
				if diff := cmp.Diff(test.state, db.State); diff != "" {
					t.Fatalf("State mismatch (-want +got): %s\n", diff)
				}
			}
		})
	}
}

// TODO: DeleteState needs tests
// TODO: DeleteState needs to be implemented in Nik's remove tunnel PRs
func TestDbSaveState(t *testing.T) {
	tests := []struct {
		name        string
		state       []*api.ProvisionRequest
		goldenFile  string
		expectError bool
	}{
		{
			name:        "provision_request_is_nil",
			state:       []*api.ProvisionRequest{nil},
			expectError: true,
		},
		{
			name: "save_edgefiltering_state_successfully",
			state: []*api.ProvisionRequest{
				{
					TunnelSrc:    net.IP{1, 1, 1, 1},
					TunnelDst:    net.IP{2, 2, 2, 2},
					TunnelNet:    &net.IPNet{IP: net.IP{169, 254, 0, 0}, Mask: net.IPMask{255, 255, 255, 254}},
					DoubleZeroIP: net.IP{7, 7, 7, 7},
					DoubleZeroPrefixes: []*net.IPNet{
						{IP: net.IP{7, 0, 0, 0}, Mask: net.IPMask{255, 0, 0, 0}},
					},
					BgpLocalAsn:  65000,
					BgpRemoteAsn: 65001,
					UserType:     api.UserTypeEdgeFiltering,
				},
			},
			goldenFile:  "./fixtures/doublezerod.edgefiltering.json",
			expectError: false,
		},
		{
			name: "save_ibrl_state_successfully",
			state: []*api.ProvisionRequest{
				{
					TunnelSrc:          net.IP{1, 1, 1, 1},
					TunnelDst:          net.IP{2, 2, 2, 2},
					TunnelNet:          &net.IPNet{IP: net.IP{169, 254, 0, 0}, Mask: net.IPMask{255, 255, 255, 254}},
					DoubleZeroIP:       net.IP{1, 1, 1, 1},
					DoubleZeroPrefixes: []*net.IPNet{},
					BgpLocalAsn:        65000,
					BgpRemoteAsn:       65001,
					UserType:           api.UserTypeIBRL,
				},
			},
			goldenFile:  "./fixtures/doublezerod.ibrl.json",
			expectError: false,
		},
		{
			name: "save_multiple_services_successfully",
			state: []*api.ProvisionRequest{
				{
					TunnelSrc:          net.IP{1, 1, 1, 1},
					TunnelDst:          net.IP{2, 2, 2, 2},
					TunnelNet:          &net.IPNet{IP: net.IP{169, 254, 0, 0}, Mask: net.IPMask{255, 255, 255, 254}},
					DoubleZeroIP:       net.IP{1, 1, 1, 1},
					DoubleZeroPrefixes: []*net.IPNet{},
					BgpLocalAsn:        65000,
					BgpRemoteAsn:       65001,
					UserType:           api.UserTypeIBRL,
				},
				{
					TunnelSrc:          net.IP{1, 1, 1, 1},
					TunnelDst:          net.IP{2, 2, 2, 2},
					TunnelNet:          &net.IPNet{IP: net.IP{169, 254, 0, 0}, Mask: net.IPMask{255, 255, 255, 254}},
					DoubleZeroIP:       net.IP{1, 1, 1, 1},
					DoubleZeroPrefixes: []*net.IPNet{},
					BgpLocalAsn:        65000,
					BgpRemoteAsn:       65001,
					UserType:           api.UserTypeMulticast,
				},
			},

			goldenFile:  "./fixtures/doublezerod.multiservice.json",
			expectError: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root, err := os.MkdirTemp("", "doublezerod")
			if err != nil {
				t.Fatalf("error creating temp dir: %v", err)
			}
			defer os.RemoveAll(root)

			// XDG_STATE_HOME is used in NewDb so use it to set a tmp dir
			t.Setenv("XDG_STATE_HOME", root)

			path := filepath.Join(root, "doublezerod")
			if err := os.Mkdir(path, 0766); err != nil {
				t.Fatalf("error creating state dir: %v", err)
			}

			db, err := manager.NewDb()
			if err != nil {
				t.Fatalf("failed to setup db: %v", err)
			}

			for _, state := range test.state {
				err = db.SaveState(state)
				if test.expectError && err == nil {
					t.Fatalf("expected error but got none")
				}
				if !test.expectError && err != nil {
					t.Fatalf("error creating db file: %v", err)
				}
				if test.expectError {
					return
				}
			}

			want, err := os.ReadFile(test.goldenFile)
			if err != nil {
				t.Fatalf("error reading goldenfile: %v", err)
			}
			got, err := os.ReadFile(db.Path)
			if err != nil {
				t.Fatalf("error reading state file: %v", err)
			}
			if diff := cmp.Diff(string(want), string(got)); diff != "" {
				t.Fatalf("State mismatch (-want +got): %s\n", diff)
			}
		})
	}

}

func TestDbDeleteState(t *testing.T) {
	root, err := os.MkdirTemp("", "doublezerod")
	if err != nil {
		t.Fatalf("error creating temp dir: %v", err)
	}
	defer os.RemoveAll(root)

	// XDG_STATE_HOME is used in NewDb so use it to set a tmp dir
	t.Setenv("XDG_STATE_HOME", root)

	path := filepath.Join(root, "doublezerod")
	if err := os.Mkdir(path, 0766); err != nil {
		t.Fatalf("error creating state dir: %v", err)
	}

	file, err := os.ReadFile("./fixtures/doublezerod.ibrl.json")
	if err != nil {
		t.Fatalf("error reading fixture: %v", err)
	}
	stateFile := filepath.Join(path, "doublezerod.json")
	// Create an empty file so we have something to delete
<<<<<<< HEAD
	err = manager.WriteFile(stateFile, file, os.FileMode(os.O_RDWR|os.O_CREATE|os.O_TRUNC))
=======
	err = manager.WriteFile(stateFile, []byte{}, os.FileMode(os.O_RDWR|os.O_CREATE|os.O_TRUNC))
>>>>>>> 118d633 (splitting out into services)
	if err != nil {
		t.Fatalf("could not create file: %v", err)
	}

	log.Printf("state: %s", filepath.Join(path, "doublezerod.json"))
	db, err := manager.NewDb()
	if err != nil {
		t.Fatalf("failed to setup db: %v", err)
	}

	u := api.UserTypeIBRL
	err = db.DeleteState(u)
	// test there is no error
	if err != nil {
		t.Fatalf("error removing state file: %v", err)
	}
	file, err = os.ReadFile(stateFile)
	if err != nil {
		t.Fatalf("error reading state file: %v", err)
	}
	if string(file) != "[]\n" {
		t.Fatalf("state file is not empty: %s", string(file))
	}

	// test the db.State has been set back to nil
	if len(db.State) != 0 {
		t.Fatalf("db state is not nil: %v", db.State)
	}
}

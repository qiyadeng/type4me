package hub

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

type State struct {
	Version  int        `json:"version"`
	Accounts []*Account `json:"accounts"`
	Devices  []*Device  `json:"devices"`
}

const stateVersion = 1

// loadState reads state from path; missing file returns empty state (version=1).
func loadState(path string) (*State, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return &State{Version: stateVersion, Accounts: []*Account{}, Devices: []*Device{}}, nil
	}
	if err != nil {
		return nil, err
	}
	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, err
	}
	if st.Version == 0 {
		st.Version = stateVersion
	}
	if st.Accounts == nil {
		st.Accounts = []*Account{}
	}
	if st.Devices == nil {
		st.Devices = []*Device{}
	}
	return &st, nil
}

// saveState atomically writes state via tmp file + rename.
func saveState(path string, st *State) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

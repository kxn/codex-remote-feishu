package issuedocsync

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"
)

func LoadState(path string, repo string) (StateFile, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return NewState(repo), nil
		}
		return StateFile{}, fmt.Errorf("read state file: %w", err)
	}

	var raw struct {
		Version int             `json:"version"`
		Repo    string          `json:"repo"`
		Issues  json.RawMessage `json:"issues"`
	}
	if err := json.Unmarshal(payload, &raw); err != nil {
		return StateFile{}, fmt.Errorf("decode state file: %w", err)
	}
	state := StateFile{
		Version: raw.Version,
		Repo:    raw.Repo,
		Issues:  map[string]IssueRecord{},
	}
	if len(raw.Issues) != 0 && string(raw.Issues) != "null" {
		var issueMap map[string]IssueRecord
		if err := json.Unmarshal(raw.Issues, &issueMap); err == nil {
			state.Issues = issueMap
		} else {
			var issueList []IssueRecord
			if err := json.Unmarshal(raw.Issues, &issueList); err != nil {
				return StateFile{}, fmt.Errorf("decode issues payload: %w", err)
			}
			for _, record := range issueList {
				state.Issues[strconv.Itoa(record.Number)] = record
			}
		}
	}
	if state.Version == 0 {
		state.Version = 1
	}
	if state.Repo == "" {
		state.Repo = repo
	}
	if state.Issues == nil {
		state.Issues = map[string]IssueRecord{}
	}
	return state, nil
}

func SaveState(path string, state StateFile) error {
	if state.Version == 0 {
		state.Version = 1
	}
	if state.Issues == nil {
		state.Issues = map[string]IssueRecord{}
	}

	sorted := sortedIssues(state.Issues)
	payload, err := json.MarshalIndent(struct {
		Version int           `json:"version"`
		Repo    string        `json:"repo"`
		Issues  []IssueRecord `json:"issues"`
	}{Version: state.Version, Repo: state.Repo, Issues: sorted}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode state file: %w", err)
	}
	payload = append(payload, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		return fmt.Errorf("write state file: %w", err)
	}
	return nil
}

func NewState(repo string) StateFile {
	return StateFile{
		Version: 1,
		Repo:    repo,
		Issues:  map[string]IssueRecord{},
	}
}

type sortedRecord struct {
	IssueRecord
}

func sortedIssues(input map[string]IssueRecord) []IssueRecord {
	keys := make([]int, 0, len(input))
	for key := range input {
		number, err := strconv.Atoi(key)
		if err != nil {
			continue
		}
		keys = append(keys, number)
	}
	sort.Ints(keys)
	out := make([]IssueRecord, 0, len(keys))
	for _, key := range keys {
		record := input[strconv.Itoa(key)]
		out = append(out, record)
	}
	return out
}

func UpsertRecord(state *StateFile, record IssueRecord) {
	if state.Version == 0 {
		state.Version = 1
	}
	if state.Issues == nil {
		state.Issues = map[string]IssueRecord{}
	}
	if record.RecordedAt == "" {
		record.RecordedAt = time.Now().UTC().Format(time.RFC3339)
	}
	state.Issues[strconv.Itoa(record.Number)] = record
}

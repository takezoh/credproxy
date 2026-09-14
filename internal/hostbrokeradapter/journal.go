package hostbrokeradapter

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

type journalRecord struct {
	Digest string `json:"digest"`
	State  string `json:"state,omitempty"`
	Output []byte `json:"output,omitempty"`
}

type journalFile struct {
	Version int                      `json:"version"`
	Records map[string]journalRecord `json:"records"`
}

type journal struct {
	mu   sync.Mutex
	path string
	file journalFile
}

func openJournal(path string) (*journal, error) {
	j := &journal{path: path, file: journalFile{Version: 1, Records: map[string]journalRecord{}}}
	if raw, err := os.ReadFile(path); err == nil {
		if json.Unmarshal(raw, &j.file) != nil || j.file.Version != 1 || j.file.Records == nil {
			return nil, errors.New("credproxy adapter journal is invalid")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return j, nil
}

func digest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (j *journal) resolve(key string, input []byte) (string, []byte, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	record, ok := j.file.Records[key]
	if !ok {
		if len(j.file.Records) >= 10_000 {
			return "", nil, errors.New("credproxy adapter journal capacity reached")
		}
		j.file.Records[key] = journalRecord{Digest: digest(input), State: "reserved"}
		if err := j.persistLocked(); err != nil {
			delete(j.file.Records, key)
			return "", nil, err
		}
		return "first", nil, nil
	}
	if record.Digest != digest(input) {
		return "input_mismatch", nil, nil
	}
	if record.State == "reserved" {
		return "mapping_unresolved", nil, nil
	}
	return "replay", append([]byte(nil), record.Output...), nil
}

func (j *journal) complete(key string, input, output []byte) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if _, exists := j.file.Records[key]; !exists && len(j.file.Records) >= 10_000 {
		return errors.New("credproxy adapter journal capacity reached")
	}
	if current, exists := j.file.Records[key]; exists && current.Digest != digest(input) {
		return errors.New("credproxy adapter journal input mismatch")
	}
	j.file.Records[key] = journalRecord{Digest: digest(input), State: "completed", Output: append([]byte(nil), output...)}
	return j.persistLocked()
}

func (j *journal) persistLocked() error {
	raw, err := json.Marshal(j.file)
	if err != nil {
		return err
	}
	if len(raw) > 64<<20 {
		return errors.New("credproxy adapter journal byte capacity reached")
	}
	if err := os.MkdirAll(filepath.Dir(j.path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(j.path), ".journal-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err = tmp.Chmod(0o600); err == nil {
		_, err = tmp.Write(raw)
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(name, j.path)
	}
	return err
}

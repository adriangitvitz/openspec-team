package team

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/adriangitvitz/openspec-go/internal/fsutil"
)

// NeedsExitCode is the exit code for a structured pause: fulfill the pending extraction requests and re-run.
const NeedsExitCode = 7

// DefaultMaxExtractionRoundTrips bounds pause/fulfill/re-run cycles per persona×artifact; --max-extraction-roundtrips overrides.
const DefaultMaxExtractionRoundTrips = 2

// NeedsFileName is the cross-process discovery file for pending requests in the change directory.
const NeedsFileName = "extraction-needs.json"

// ExtractionRequest is one model ask: extract detail from a binary document.
type ExtractionRequest struct {
	Path      string `json:"path"`
	Detail    string `json:"detail"`
	Rationale string `json:"rationale"`
}

// NeedsPayload is what a paused run emits on stdout and persists.
type NeedsPayload struct {
	ChangeName string              `json:"changeName"`
	Persona    string              `json:"persona"`
	Artifact   string              `json:"artifact"`
	RoundTrip  int                 `json:"roundTrip"`
	Requests   []ExtractionRequest `json:"requests"`
}

// ExtractionNeeded is the typed pause error from RunOpenRouter; unwrap with errors.As and exit with NeedsExitCode.
type ExtractionNeeded struct {
	Payload NeedsPayload
}

func (e *ExtractionNeeded) Error() string {
	return fmt.Sprintf("run paused: %d extraction request(s) pending (round trip %d); fulfill them and re-run",
		len(e.Payload.Requests), e.Payload.RoundTrip)
}

// needsFile is the on-disk state, keyed persona/artifact; counts survive clearing so the cap holds across fulfillment cycles.
type needsFile struct {
	Counts  map[string]int                 `json:"counts"`
	Pending map[string][]ExtractionRequest `json:"pending"`
}

func needsKey(persona, artifact string) string { return persona + "/" + artifact }

// withNeedsLock serializes read-modify-write on the needs file across processes; atomic writes alone would lose concurrent updates.
func withNeedsLock(changeDir string, fn func() error) error {
	lockPath := filepath.Join(changeDir, NeedsFileName+".lock")
	const (
		retries    = 100
		retryEvery = 20 * time.Millisecond
		staleAfter = 10 * time.Second
	)
	for i := 0; i < retries; i++ {
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			f.Close()
			defer os.Remove(lockPath)
			return fn()
		}
		if info, statErr := os.Stat(lockPath); statErr == nil && time.Since(info.ModTime()) > staleAfter {
			os.Remove(lockPath)
			continue
		}
		time.Sleep(retryEvery)
	}
	return fmt.Errorf("could not lock %s after %v: another run holds it, or remove the stale lock", lockPath, time.Duration(retries)*retryEvery)
}

// loadNeeds distinguishes an absent file (clean state) from a corrupt one: the round-trip cap fails closed, never silently resets.
func loadNeeds(changeDir string) (needsFile, error) {
	nf := needsFile{Counts: map[string]int{}, Pending: map[string][]ExtractionRequest{}}
	content, err := os.ReadFile(filepath.Join(changeDir, NeedsFileName))
	if os.IsNotExist(err) {
		return nf, nil
	}
	if err != nil {
		return nf, err
	}
	if err := json.Unmarshal(content, &nf); err != nil {
		return nf, fmt.Errorf("%s is corrupt: %w — fix or delete it", NeedsFileName, err)
	}
	if nf.Counts == nil {
		nf.Counts = map[string]int{}
	}
	if nf.Pending == nil {
		nf.Pending = map[string][]ExtractionRequest{}
	}
	return nf, nil
}

// saveNeeds writes the file, or removes it when nothing remains.
func saveNeeds(changeDir string, nf needsFile) error {
	if len(nf.Counts) == 0 && len(nf.Pending) == 0 {
		err := os.Remove(filepath.Join(changeDir, NeedsFileName))
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	content, err := json.MarshalIndent(nf, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(filepath.Join(changeDir, NeedsFileName), content, 0o644)
}

// extractionRoundTrips returns the persisted count; a corrupt needs file is an error, not a zero.
func extractionRoundTrips(changeDir, persona, artifact string) (int, error) {
	nf, err := loadNeeds(changeDir)
	if err != nil {
		return 0, err
	}
	return nf.Counts[needsKey(persona, artifact)], nil
}

// recordPause persists a batch of requests as one round trip and returns the new count.
func recordPause(changeDir, persona, artifact string, reqs []ExtractionRequest) (int, error) {
	var count int
	err := withNeedsLock(changeDir, func() error {
		nf, err := loadNeeds(changeDir)
		if err != nil {
			return err
		}
		key := needsKey(persona, artifact)
		nf.Counts[key]++
		nf.Pending[key] = reqs
		count = nf.Counts[key]
		return saveNeeds(changeDir, nf)
	})
	return count, err
}

// ClearPendingExtractions marks a persona×artifact's requests fulfilled after a successful run, retaining the round-trip count.
func ClearPendingExtractions(changeDir, persona, artifact string) error {
	return withNeedsLock(changeDir, func() error {
		nf, err := loadNeeds(changeDir)
		if err != nil {
			return err
		}
		key := needsKey(persona, artifact)
		if _, ok := nf.Pending[key]; !ok {
			return nil
		}
		delete(nf.Pending, key)
		return saveNeeds(changeDir, nf)
	})
}
